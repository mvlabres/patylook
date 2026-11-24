package service

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder

	xdraw "golang.org/x/image/draw"

	"patylook/internal/core/domain"
)

const (

	// maxImagePixels guards against decompression bombs (checked before decoding).
	maxImagePixels = 25_000_000
	minImageSide   = 200
	// maxImageSide is the longest side kept; larger images are scaled down.
	maxImageSide = 1600
	jpegQuality  = 85
)

var (
	errImageTooBig   = fmt.Errorf("imagem acima do limite de %d MB", domain.MaxImageBytes>>20)
	errImageFormat   = errors.New("formato não suportado: envie JPEG ou PNG")
	errImageCorrupt  = errors.New("arquivo de imagem inválido ou corrompido")
	errImagePixels   = errors.New("dimensões da imagem excedem o limite permitido")
	errImageTooSmall = fmt.Errorf("imagem muito pequena: mínimo de %dpx no menor lado", minImageSide)
)

// normalizeImage validates an uploaded JPEG/PNG and returns it re-encoded as JPEG:
// EXIF orientation applied, transparency flattened on white, longest side limited and
// all metadata dropped. The content, not the file name or declared type, decides validity.
func normalizeImage(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errImageCorrupt
	}
	if len(data) > domain.MaxImageBytes {
		return nil, errImageTooBig
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return nil, errImageFormat
		}
		return nil, errImageCorrupt
	}
	if format != "jpeg" && format != "png" {
		return nil, errImageFormat
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxImagePixels {
		return nil, errImagePixels
	}
	if cfg.Width < minImageSide || cfg.Height < minImageSide {
		return nil, errImageTooSmall
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errImageCorrupt
	}

	w, h := scaledSize(cfg.Width, cfg.Height)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if w == cfg.Width && h == cfg.Height {
		draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	}

	if format == "jpeg" {
		dst = applyOrientation(dst, jpegOrientation(data))
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("codificar imagem: %w", err)
	}
	return out.Bytes(), nil
}

// scaledSize shrinks (w, h) so the longest side is at most maxImageSide.
func scaledSize(w, h int) (int, int) {
	longest := max(w, h)
	if longest <= maxImageSide {
		return w, h
	}
	nw := max(1, w*maxImageSide/longest)
	nh := max(1, h*maxImageSide/longest)
	return nw, nh
}

// jpegOrientation reads the EXIF orientation tag (1-8) of a JPEG. Any parsing problem
// yields 1 (no transformation): a malformed tag must never reject a valid image.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xFF {
			return 1
		}
		marker := data[pos+1]
		if marker == 0xFF { // fill byte
			pos++
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan / end of image
			return 1
		}
		size := int(data[pos+2])<<8 | int(data[pos+3])
		if size < 2 || pos+2+size > len(data) {
			return 1
		}
		if marker == 0xE1 {
			if o := exifOrientation(data[pos+4 : pos+2+size]); o != 0 {
				return o
			}
		}
		pos += 2 + size
	}
	return 1
}

// exifOrientation parses an APP1 payload ("Exif\0\0" + TIFF) and returns the orientation or 0.
func exifOrientation(seg []byte) int {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 0
	}
	tiff := seg[6:]
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(tiff[:2]) {
	case "II":
		u16 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
		u32 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24 }
	case "MM":
		u16 = func(b []byte) int { return int(b[0])<<8 | int(b[1]) }
		u32 = func(b []byte) int { return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3]) }
	default:
		return 0
	}
	if u16(tiff[2:4]) != 0x002A {
		return 0
	}
	ifd := u32(tiff[4:8])
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}
	count := u16(tiff[ifd : ifd+2])
	for i := 0; i < count; i++ {
		entry := ifd + 2 + i*12
		if entry+12 > len(tiff) {
			return 0
		}
		if u16(tiff[entry:entry+2]) == 0x0112 { // Orientation
			if v := u16(tiff[entry+8 : entry+10]); v >= 1 && v <= 8 {
				return v
			}
			return 0
		}
	}
	return 0
}

// applyOrientation rotates/flips img according to an EXIF orientation value.
func applyOrientation(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			si := y*src.Stride + x*4
			di := dy*dst.Stride + dx*4
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
