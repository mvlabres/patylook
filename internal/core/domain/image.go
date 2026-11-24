package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	// ProductImagesBase is the path prefix stored in product_images.path. The folder of a
	// product is ProductImagesBase + "/" + ImageFolder(sku).
	ProductImagesBase = "images/products"
	// MainImageName is the fixed file name of a product's main image (the showcase banner).
	MainImageName = "principal.jpg"
	// MaxExtraImages caps the carousel images (besides the main one) per product.
	MaxExtraImages = 8
	// MaxImageBytes is the largest accepted upload per image.
	MaxImageBytes = 6 << 20
)

// ProductImage is a registered image file of a product.
type ProductImage struct {
	ID        int
	ProductID int
	FileName  string
	Path      string // relative, e.g. images/products/CAM-1/principal.jpg
	SortOrder int
	CreatedBy int
	CreatedAt time.Time
}

func (i ProductImage) IsMain() bool { return i.FileName == MainImageName }

// URL is the address the application serves the image from.
func (i ProductImage) URL() string { return "/" + i.Path }

var unsafeFolderChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

var errEmptyFolder = errors.New("SKU não gera um nome de pasta válido")

// ImageFolder derives the folder name for a SKU: characters that are unsafe in file
// system paths become "-". Different SKUs may collapse to the same folder ("A/B" and
// "A B"); the unique index on product_images.path rejects such a collision.
func ImageFolder(sku string) (string, error) {
	folder := unsafeFolderChars.ReplaceAllString(strings.TrimSpace(sku), "-")
	folder = strings.Trim(folder, "-.")
	if folder == "" {
		return "", errEmptyFolder
	}
	return folder, nil
}

// ImagePath builds the stored path of a file inside a product folder.
func ImagePath(folder, fileName string) string {
	return ProductImagesBase + "/" + folder + "/" + fileName
}

// ImageFolderFromPath extracts the product folder from a stored path.
func ImageFolderFromPath(path string) string {
	rest := strings.TrimPrefix(path, ProductImagesBase+"/")
	folder, _, _ := strings.Cut(rest, "/")
	return folder
}
