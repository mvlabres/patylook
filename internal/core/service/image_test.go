package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"sort"
	"testing"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// ---- image helpers ----

func solid(w, h int, c color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// withOrientation injects an EXIF APP1 segment (little endian) right after the JPEG SOI marker.
func withOrientation(jpg []byte, orientation byte) []byte {
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 8, 0, 0, 0, // header, IFD0 at offset 8
		1, 0, // one entry
		0x12, 0x01, 3, 0, 1, 0, 0, 0, orientation, 0, 0, 0, // tag 0x0112, SHORT, count 1
		0, 0, 0, 0, // next IFD
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	size := len(payload) + 2
	seg := append([]byte{0xFF, 0xE1, byte(size >> 8), byte(size)}, payload...)
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "jpeg" {
		t.Fatalf("output is not a valid JPEG: %v (%s)", err, format)
	}
	return img
}

// near reports whether a pixel is close to the expected color (JPEG is lossy).
func near(c color.Color, r, g, b uint32) bool {
	cr, cg, cb, _ := c.RGBA()
	d := func(a, b uint32) uint32 {
		if a > b {
			return (a - b) >> 8
		}
		return (b - a) >> 8
	}
	return d(cr, r<<8) < 40 && d(cg, g<<8) < 40 && d(cb, b<<8) < 40
}

func TestNormalizeImageConvertsPNGToJPEG(t *testing.T) {
	out, err := normalizeImage(encodePNG(t, solid(400, 300, color.RGBA{R: 255, A: 255})))
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	if img.Bounds().Dx() != 400 || img.Bounds().Dy() != 300 {
		t.Errorf("size = %v", img.Bounds())
	}
	if !near(img.At(200, 150), 255, 0, 0) {
		t.Errorf("color changed: %v", img.At(200, 150))
	}
}

func TestNormalizeImageFlattensTransparencyOnWhite(t *testing.T) {
	out, err := normalizeImage(encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 300, 300)))) // fully transparent
	if err != nil {
		t.Fatal(err)
	}
	if !near(decode(t, out).At(10, 10), 255, 255, 255) {
		t.Error("transparent pixels must become white, not black")
	}
}

func TestNormalizeImageScalesDown(t *testing.T) {
	out, err := normalizeImage(encodeJPEG(t, solid(3200, 2400, color.RGBA{B: 255, A: 255})))
	if err != nil {
		t.Fatal(err)
	}
	b := decode(t, out).Bounds()
	if b.Dx() != 1600 || b.Dy() != 1200 {
		t.Errorf("scaled size = %v", b)
	}
}

func TestNormalizeImageAppliesExifOrientation(t *testing.T) {
	// 300x200 image, left half red and right half blue.
	src := solid(300, 200, color.RGBA{B: 255, A: 255})
	for y := 0; y < 200; y++ {
		for x := 0; x < 150; x++ {
			src.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	jpg := encodeJPEG(t, src)

	tests := []struct {
		name        string
		orientation byte
		w, h        int
		// pixel in the output that must be red
		redX, redY int
	}{
		{"unchanged", 1, 300, 200, 20, 100},
		{"mirrored", 2, 300, 200, 280, 100},
		{"rotate 180", 3, 300, 200, 280, 100},
		{"rotate 90 cw", 6, 200, 300, 100, 20},   // red (left) moves to the top
		{"rotate 90 ccw", 8, 200, 300, 100, 280}, // red (left) moves to the bottom
	}
	for _, tt := range tests {
		out, err := normalizeImage(withOrientation(jpg, tt.orientation))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		img := decode(t, out)
		if img.Bounds().Dx() != tt.w || img.Bounds().Dy() != tt.h {
			t.Errorf("%s: size %v, want %dx%d", tt.name, img.Bounds(), tt.w, tt.h)
			continue
		}
		if !near(img.At(tt.redX, tt.redY), 255, 0, 0) {
			t.Errorf("%s: pixel (%d,%d) = %v, want red", tt.name, tt.redX, tt.redY, img.At(tt.redX, tt.redY))
		}
	}
}

func TestMalformedExifDoesNotRejectImage(t *testing.T) {
	jpg := encodeJPEG(t, solid(300, 300, color.RGBA{G: 255, A: 255}))
	broken := append([]byte{}, jpg[:2]...)
	broken = append(broken, 0xFF, 0xE1, 0x00, 0x10, 'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 0x2A, 0, 0xFF, 0xFF, 0xFF, 0xFF) // bogus offsets
	broken = append(broken, jpg[2:]...)
	if _, err := normalizeImage(broken); err != nil {
		t.Errorf("valid image rejected because of bad EXIF: %v", err)
	}
}

func TestNormalizeImageRejections(t *testing.T) {
	var gif bytes.Buffer
	gif.WriteString("GIF89a")
	gif.Write(make([]byte, 64))

	cases := map[string][]byte{
		"empty":         nil,
		"text":          []byte("not an image at all"),
		"gif":           gif.Bytes(),
		"truncated png": encodePNG(t, solid(300, 300, color.White))[:60],
		"tiny":          encodePNG(t, solid(50, 50, color.White)),
		"too big":       make([]byte, domain.MaxImageBytes+1),
	}
	for name, data := range cases {
		if _, err := normalizeImage(data); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if _, err := normalizeImage(append([]byte("<script>"), encodePNG(t, solid(300, 300, color.White))...)); err == nil {
		t.Error("content with a prepended payload must not pass as an image")
	}
}

// ---- catalog service with in-memory fakes ----

type memImages struct {
	port.ProductImageRepository
	rows   map[int]*domain.ProductImage
	nextID int
}

func (m *memImages) Create(_ context.Context, img *domain.ProductImage) error {
	for _, r := range m.rows {
		if r.Path == img.Path {
			return &domain.ConflictError{Field: "path"}
		}
	}
	m.nextID++
	img.ID = m.nextID
	c := *img
	m.rows[img.ID] = &c
	return nil
}

func (m *memImages) ListByProduct(_ context.Context, productID int) ([]domain.ProductImage, error) {
	var out []domain.ProductImage
	for _, r := range m.rows {
		if r.ProductID == productID {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memImages) FindByID(_ context.Context, id int) (*domain.ProductImage, error) {
	if r, ok := m.rows[id]; ok {
		c := *r
		return &c, nil
	}
	return nil, domain.ErrNotFound
}

func (m *memImages) Delete(_ context.Context, id int) error {
	delete(m.rows, id)
	return nil
}

type memStorage struct {
	files    map[string][]byte // "folder/name"
	failSave string            // file name whose Save fails
}

func (m *memStorage) Save(_ context.Context, folder, name string, data []byte) error {
	if name == m.failSave {
		return errors.New("disk full")
	}
	m.files[folder+"/"+name] = data
	return nil
}

func (m *memStorage) Remove(_ context.Context, folder, name string) error {
	delete(m.files, folder+"/"+name)
	return nil
}

func (m *memStorage) Open(context.Context, string, string) (io.ReadSeekCloser, time.Time, error) {
	return nil, time.Time{}, domain.ErrNotFound
}

// catalogProducts is a product repository that assigns ids and enforces unique SKUs.
type catalogProducts struct {
	port.ProductRepository
	items  map[int]*domain.Product
	nextID int
}

func (m *catalogProducts) Create(_ context.Context, p *domain.Product) error {
	for _, e := range m.items {
		if e.SKU == p.SKU {
			return &domain.ConflictError{Field: "sku"}
		}
	}
	m.nextID++
	p.ID = m.nextID
	c := *p
	m.items[p.ID] = &c
	return nil
}

func (m *catalogProducts) FindByID(_ context.Context, id int) (*domain.Product, error) {
	if p, ok := m.items[id]; ok {
		c := *p
		return &c, nil
	}
	return nil, domain.ErrNotFound
}

func (m *catalogProducts) Update(_ context.Context, p *domain.Product) error {
	c := *p
	m.items[p.ID] = &c
	return nil
}

// snapshotTx emulates a transaction: rows created inside fn disappear when fn fails.
type snapshotTx struct {
	products *catalogProducts
	images   *memImages
}

func (s *snapshotTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	products := map[int]*domain.Product{}
	for k, v := range s.products.items {
		c := *v
		products[k] = &c
	}
	images := map[int]*domain.ProductImage{}
	for k, v := range s.images.rows {
		c := *v
		images[k] = &c
	}
	if err := fn(ctx); err != nil {
		s.products.items, s.images.rows = products, images
		return err
	}
	return nil
}

type catalogEnv struct {
	svc      *CatalogService
	products *catalogProducts
	images   *memImages
	storage  *memStorage
}

func newCatalogEnv() *catalogEnv {
	products := &catalogProducts{items: map[int]*domain.Product{}}
	images := &memImages{rows: map[int]*domain.ProductImage{}}
	storage := &memStorage{files: map[string][]byte{}}
	clock := newClock()
	return &catalogEnv{
		svc: NewCatalogService(nil, products, nil, images, storage, &snapshotTx{products, images}, clock.now,
			slog.New(slog.NewTextHandler(io.Discard, nil))),
		products: products, images: images, storage: storage,
	}
}

var uploader = &domain.User{ID: 5}

func upload(t testing.TB, name string) port.ImageUpload {
	t.Helper()
	return port.ImageUpload{Filename: name, Data: encodePNG(t, solid(400, 300, color.RGBA{R: 200, A: 255}))}
}

func newProduct(sku string) *domain.Product {
	return &domain.Product{Name: "Camiseta", SKU: sku, CategoryID: 1, Stock: 4}
}

func (e *catalogEnv) fileNames() []string {
	var out []string
	for k := range e.storage.files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestCreateProductStoresImages(t *testing.T) {
	env := newCatalogEnv()
	main := upload(t, "frente.png")
	err := env.svc.CreateProduct(context.Background(), uploader, newProduct(" cam 1/x "), port.ProductImages{
		Main:  &main,
		Extra: []port.ImageUpload{upload(t, "a.png"), upload(t, "b.png")},
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"CAM-1-X/imagem-01.jpg", "CAM-1-X/imagem-02.jpg", "CAM-1-X/principal.jpg"}
	if got := env.fileNames(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	for name, data := range env.storage.files {
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			t.Errorf("%s is not a JPEG: %v", name, err)
		}
	}
	rows, _ := env.images.ListByProduct(context.Background(), 1)
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	byName := map[string]domain.ProductImage{}
	for _, r := range rows {
		byName[r.FileName] = r
	}
	if r := byName["principal.jpg"]; r.Path != "images/products/CAM-1-X/principal.jpg" || r.CreatedBy != 5 || !r.IsMain() {
		t.Errorf("main row: %+v", r)
	}
	if r := byName["imagem-02.jpg"]; r.SortOrder != 2 || r.Path != "images/products/CAM-1-X/imagem-02.jpg" {
		t.Errorf("extra row: %+v", r)
	}
}

func TestCreateProductRequiresValidMainImage(t *testing.T) {
	cases := map[string]port.ProductImages{
		"no main":      {},
		"only extras":  {Extra: []port.ImageUpload{upload(t, "a.png")}},
		"corrupt main": {Main: &port.ImageUpload{Filename: "x.png", Data: []byte("garbage")}},
	}
	for name, imgs := range cases {
		env := newCatalogEnv()
		err := env.svc.CreateProduct(context.Background(), uploader, newProduct("SKU1"), imgs)
		var ve *domain.ValidationError
		if !errors.As(err, &ve) || ve.Fields["main_image"] == "" {
			t.Errorf("%s: expected main_image error, got %v", name, err)
		}
		if len(env.products.items) != 0 || len(env.storage.files) != 0 {
			t.Errorf("%s: nothing must be persisted", name)
		}
	}
}

func TestCreateProductReportsBadExtraImages(t *testing.T) {
	env := newCatalogEnv()
	main := upload(t, "m.png")
	err := env.svc.CreateProduct(context.Background(), uploader, newProduct("SKU1"), port.ProductImages{
		Main: &main, Extra: []port.ImageUpload{{Filename: "bad.txt", Data: []byte("text")}},
	})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Fields["images"] == "" {
		t.Fatalf("expected images error, got %v", err)
	}
}

func TestCreateProductRollsBackWhenDiskFails(t *testing.T) {
	env := newCatalogEnv()
	env.storage.failSave = "imagem-01.jpg"
	main := upload(t, "m.png")
	err := env.svc.CreateProduct(context.Background(), uploader, newProduct("SKU1"), port.ProductImages{
		Main: &main, Extra: []port.ImageUpload{upload(t, "a.png")},
	})
	if err == nil {
		t.Fatal("expected failure")
	}
	if len(env.products.items) != 0 || len(env.images.rows) != 0 {
		t.Error("database rows must be rolled back")
	}
	if len(env.storage.files) != 0 {
		t.Errorf("files already written must be removed, left: %v", env.fileNames())
	}
}

func TestCreateProductDuplicateSKUWritesNothing(t *testing.T) {
	env := newCatalogEnv()
	main := upload(t, "m.png")
	imgs := port.ProductImages{Main: &main}
	if err := env.svc.CreateProduct(context.Background(), uploader, newProduct("DUP"), imgs); err != nil {
		t.Fatal(err)
	}
	before := env.fileNames()
	err := env.svc.CreateProduct(context.Background(), uploader, newProduct("DUP"), imgs)
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Fields["sku"] == "" {
		t.Fatalf("expected sku error, got %v", err)
	}
	if fmt.Sprint(env.fileNames()) != fmt.Sprint(before) {
		t.Error("a rejected product must not touch existing files")
	}
}

func TestCreateProductRejectsUnusableSKU(t *testing.T) {
	env := newCatalogEnv()
	main := upload(t, "m.png")
	err := env.svc.CreateProduct(context.Background(), uploader, newProduct("///"), port.ProductImages{Main: &main})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Fields["sku"] == "" {
		t.Errorf("expected sku error, got %v", err)
	}
}

func TestAddAndDeleteImages(t *testing.T) {
	env := newCatalogEnv()
	ctx := context.Background()
	// A product that predates the image feature: registered without images.
	env.products.items[1] = &domain.Product{ID: 1, SKU: "OLD-1", Name: "Antigo", IsActive: true}
	env.products.nextID = 1

	// The main image has to come first.
	err := env.svc.AddImages(ctx, uploader, 1, port.ProductImages{Extra: []port.ImageUpload{upload(t, "a.png")}})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Fields["main_image"] == "" {
		t.Fatalf("extras before main: %v", err)
	}

	main := upload(t, "m.png")
	if err := env.svc.AddImages(ctx, uploader, 1, port.ProductImages{Main: &main, Extra: []port.ImageUpload{upload(t, "a.png")}}); err != nil {
		t.Fatal(err)
	}
	// Further uploads continue the numbering and replacing the main image reuses its row.
	main2 := upload(t, "m2.png")
	if err := env.svc.AddImages(ctx, uploader, 1, port.ProductImages{Main: &main2, Extra: []port.ImageUpload{upload(t, "b.png")}}); err != nil {
		t.Fatal(err)
	}
	rows, _ := env.images.ListByProduct(ctx, 1)
	var names []string
	for _, r := range rows {
		names = append(names, r.FileName)
	}
	if fmt.Sprint(names) != "[principal.jpg imagem-01.jpg imagem-02.jpg]" {
		t.Errorf("rows = %v", names)
	}

	// The limit counts carousel images already stored.
	var many []port.ImageUpload
	for i := 0; i < domain.MaxExtraImages; i++ {
		many = append(many, upload(t, "x.png"))
	}
	if err := env.svc.AddImages(ctx, uploader, 1, port.ProductImages{Extra: many}); err == nil {
		t.Error("exceeding the carousel limit must fail")
	}

	// The main image cannot be removed, a carousel image can.
	if err := env.svc.DeleteImage(ctx, 1, rows[0].ID); err == nil {
		t.Error("main image deletion must be refused")
	}
	if err := env.svc.DeleteImage(ctx, 2, rows[1].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("image of another product: %v", err)
	}
	if err := env.svc.DeleteImage(ctx, 1, rows[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := env.storage.files["OLD-1/imagem-01.jpg"]; ok {
		t.Error("file must be removed with its row")
	}
	if _, ok := env.storage.files["OLD-1/principal.jpg"]; !ok {
		t.Error("main file must stay")
	}
}

func TestSKUIsLockedWhileProductHasImages(t *testing.T) {
	env := newCatalogEnv()
	ctx := context.Background()
	main := upload(t, "m.png")
	if err := env.svc.CreateProduct(ctx, uploader, newProduct("LOCK-1"), port.ProductImages{Main: &main}); err != nil {
		t.Fatal(err)
	}

	changed := newProduct("OTHER")
	changed.ID = 1
	err := env.svc.UpdateProduct(ctx, changed)
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Fields["sku"] == "" {
		t.Fatalf("expected sku lock, got %v", err)
	}

	same := newProduct("LOCK-1")
	same.ID, same.Name = 1, "Novo nome"
	if err := env.svc.UpdateProduct(ctx, same); err != nil {
		t.Errorf("editing other fields must work: %v", err)
	}

	// Without images the SKU can change freely.
	env.products.items[2] = &domain.Product{ID: 2, SKU: "NOIMG", Name: "x"}
	free := newProduct("NOIMG-2")
	free.ID = 2
	if err := env.svc.UpdateProduct(ctx, free); err != nil {
		t.Errorf("sku change without images: %v", err)
	}
}
