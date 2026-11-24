package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// preparedImage is a validated, normalized (JPEG) image ready to be stored.
type preparedImage struct {
	name      string
	data      []byte
	sortOrder int
	isMain    bool
}

var extraImageName = regexp.MustCompile(`^imagem-(\d+)\.jpg$`)

func invalidField(field, msg string) error {
	v := domain.NewValidationError()
	v.Add(field, msg)
	return v
}

// imageConflictError reports a unique-key collision of image rows (two SKUs mapping to the same folder).
func imageConflictError(err error) error {
	var ce *domain.ConflictError
	if errors.As(err, &ce) {
		return invalidField("main_image", "Já existe um produto com a mesma pasta de imagens (SKU equivalente)")
	}
	return err
}

// prepareFiles validates and normalizes every upload. existing lists the images the
// product already has, so new carousel files continue the numbering. Errors are
// reported per form field ("main_image" and "images").
func prepareFiles(imgs port.ProductImages, existing []domain.ProductImage) ([]preparedImage, error) {
	v := domain.NewValidationError()
	var out []preparedImage

	if imgs.Main != nil {
		data, err := normalizeImage(imgs.Main.Data)
		if err != nil {
			v.Add("main_image", fmt.Sprintf("%s: %s", imgs.Main.Filename, err))
		} else {
			out = append(out, preparedImage{name: domain.MainImageName, data: data, isMain: true})
		}
	}

	extras, next := 0, 1
	for _, e := range existing {
		if e.IsMain() {
			continue
		}
		extras++
		if m := extraImageName.FindStringSubmatch(e.FileName); m != nil {
			if n, _ := strconv.Atoi(m[1]); n >= next {
				next = n + 1
			}
		}
	}
	if extras+len(imgs.Extra) > domain.MaxExtraImages {
		v.Add("images", fmt.Sprintf("Máximo de %d imagens adicionais por produto", domain.MaxExtraImages))
	} else {
		for _, up := range imgs.Extra {
			data, err := normalizeImage(up.Data)
			if err != nil {
				v.Add("images", fmt.Sprintf("%s: %s", up.Filename, err))
				continue
			}
			out = append(out, preparedImage{name: fmt.Sprintf("imagem-%02d.jpg", next), data: data, sortOrder: next})
			next++
		}
	}
	return out, v.OrNil()
}

// storeImages registers the rows and writes the files. It must run inside a transaction:
// a failure rolls the rows back, and the caller removes the files listed in written.
// A main image whose row already exists is only overwritten on disk.
func (s *CatalogService) storeImages(ctx context.Context, actor *domain.User, productID int, folder string,
	files []preparedImage, mainRegistered bool, written *[]string) error {
	for _, f := range files {
		overwrite := f.isMain && mainRegistered
		if !overwrite {
			row := &domain.ProductImage{
				ProductID: productID,
				FileName:  f.name,
				Path:      domain.ImagePath(folder, f.name),
				SortOrder: f.sortOrder,
				CreatedBy: actor.ID,
			}
			if err := s.images.Create(ctx, row); err != nil {
				return err
			}
		}
		if err := s.storage.Save(ctx, folder, f.name, f.data); err != nil {
			return fmt.Errorf("gravar imagem %s: %w", f.name, err)
		}
		if !overwrite {
			*written = append(*written, f.name)
		}
	}
	return nil
}

func (s *CatalogService) removeFiles(ctx context.Context, folder string, names []string) {
	for _, name := range names {
		if err := s.storage.Remove(ctx, folder, name); err != nil {
			s.log.Warn("remover arquivo de imagem órfão", "folder", folder, "file", name, "err", err)
		}
	}
}

func (s *CatalogService) ListImages(ctx context.Context, productID int) ([]domain.ProductImage, error) {
	return s.images.ListByProduct(ctx, productID)
}

func (s *CatalogService) AddImages(ctx context.Context, actor *domain.User, productID int, imgs port.ProductImages) error {
	product, err := s.products.FindByID(ctx, productID)
	if err != nil {
		return err
	}
	if imgs.Main == nil && len(imgs.Extra) == 0 {
		return invalidField("images", "Selecione ao menos uma imagem")
	}
	existing, err := s.images.ListByProduct(ctx, productID)
	if err != nil {
		return err
	}
	mainRegistered := false
	for _, e := range existing {
		mainRegistered = mainRegistered || e.IsMain()
	}
	if !mainRegistered && imgs.Main == nil {
		return invalidField("main_image", "Envie primeiro a imagem principal (principal.jpg) do produto")
	}
	folder, err := domain.ImageFolder(product.SKU)
	if err != nil {
		return invalidField("images", "SKU não gera um nome de pasta válido")
	}
	files, err := prepareFiles(imgs, existing)
	if err != nil {
		return err
	}

	var written []string
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		return s.storeImages(ctx, actor, productID, folder, files, mainRegistered, &written)
	})
	if err != nil {
		s.removeFiles(ctx, folder, written)
		return imageConflictError(err)
	}
	return nil
}

func (s *CatalogService) DeleteImage(ctx context.Context, productID, imageID int) error {
	img, err := s.images.FindByID(ctx, imageID)
	if err != nil {
		return err
	}
	if img.ProductID != productID {
		return domain.ErrNotFound
	}
	if img.IsMain() {
		return invalidField("", "A imagem principal não pode ser removida; envie outra para substituí-la")
	}
	if err := s.images.Delete(ctx, imageID); err != nil {
		return err
	}
	// The row is gone; a leftover file is harmless, so a failure here is only logged.
	if err := s.storage.Remove(ctx, domain.ImageFolderFromPath(img.Path), img.FileName); err != nil {
		s.log.Warn("remover arquivo de imagem", "path", img.Path, "err", err)
	}
	return nil
}

func (s *CatalogService) Showcase(ctx context.Context, page int, search string) (port.Page[port.ShowcaseItem], error) {
	page, lp := listParams(page, search)
	lp.OnlyActive = true
	products, total, err := s.products.List(ctx, lp, dateOf(s.now()))
	if err != nil {
		return port.Page[port.ShowcaseItem]{}, err
	}
	ids := make([]int, len(products))
	for i, p := range products {
		ids[i] = p.ID
	}
	images, err := s.images.ListByProducts(ctx, ids)
	if err != nil {
		return port.Page[port.ShowcaseItem]{}, err
	}
	items := make([]port.ShowcaseItem, len(products))
	for i, p := range products {
		items[i] = port.ShowcaseItem{Product: p, Images: images[p.ID]}
	}
	return newPage(items, total, page), nil
}

func (s *CatalogService) OpenImage(ctx context.Context, folder, name string) (io.ReadSeekCloser, time.Time, error) {
	return s.storage.Open(ctx, folder, name)
}
