package service

import (
	"context"
	"errors"
	"log/slog"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type CatalogService struct {
	categories port.ProductCategoryRepository
	brands     port.BrandRepository
	products   port.ProductRepository
	prices     port.PricebookRepository
	images     port.ProductImageRepository
	storage    port.ImageStorage
	tx         port.Transactor
	now        Clock
	log        *slog.Logger
}

var _ port.CatalogService = (*CatalogService)(nil)

func NewCatalogService(
	c port.ProductCategoryRepository, b port.BrandRepository, p port.ProductRepository, pb port.PricebookRepository,
	images port.ProductImageRepository, storage port.ImageStorage, tx port.Transactor, now Clock, log *slog.Logger,
) *CatalogService {
	return &CatalogService{categories: c, brands: b, products: p, prices: pb, images: images, storage: storage, tx: tx, now: now, log: log}
}

var (
	categoryConflicts = map[string]string{"name": "Já existe uma categoria com esse nome"}
	// The SKU is generated from category, brand and the minute of creation, so a collision means
	// the same category/brand pair was registered within the same minute.
	productConflicts = map[string]string{"sku": "Já existe um produto com esse SKU (mesma categoria e marca no mesmo minuto). Aguarde um minuto e tente novamente"}
)

func (s *CatalogService) ListCategories(ctx context.Context) ([]domain.ProductCategory, error) {
	return s.categories.List(ctx)
}

func (s *CatalogService) GetCategory(ctx context.Context, id int) (*domain.ProductCategory, error) {
	return s.categories.FindByID(ctx, id)
}

func (s *CatalogService) SaveCategory(ctx context.Context, c *domain.ProductCategory) error {
	c.Normalize()
	if err := c.Validate(); err != nil {
		return err
	}
	var err error
	if c.ID == 0 {
		err = s.categories.Create(ctx, c)
	} else {
		err = s.categories.Update(ctx, c)
	}
	return conflictToValidation(err, categoryConflicts)
}

func (s *CatalogService) DeleteCategory(ctx context.Context, id int) error {
	return s.categories.Delete(ctx, id)
}

func (s *CatalogService) ListProducts(ctx context.Context, page int, search string) (port.Page[domain.Product], error) {
	page, lp := listParams(page, search)
	items, total, err := s.products.List(ctx, lp, dateOf(s.now()))
	if err != nil {
		return port.Page[domain.Product]{}, err
	}
	return newPage(items, total, page), nil
}

func (s *CatalogService) GetProduct(ctx context.Context, id int) (*domain.Product, error) {
	return s.products.FindByID(ctx, id)
}

func (s *CatalogService) CreateProduct(ctx context.Context, actor *domain.User, p *domain.Product, imgs port.ProductImages) error {
	p.Normalize()
	p.SKU = "" // never typed: generated below from category, brand and the creation time
	v := domain.NewValidationError()
	if err := p.Validate(); err != nil {
		mergeValidation(v, err)
	}
	if p.BrandID == 0 {
		v.Add("brand_id", "Marca é obrigatória")
	}

	var category, brand string
	if p.CategoryID > 0 {
		c, err := s.categories.FindByID(ctx, p.CategoryID)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			v.Add("product_category_id", "Categoria não encontrada")
		case err != nil:
			return err
		default:
			category = c.Name
		}
	}
	if p.BrandID > 0 {
		name, err := s.activeBrandName(ctx, p.BrandID)
		if err != nil {
			if !errors.As(err, new(*domain.ValidationError)) {
				return err
			}
			mergeValidation(v, err)
		}
		brand = name
	}
	var folder string
	if category != "" && brand != "" {
		p.SKU = domain.GenerateSKU(category, brand, s.now())
		var err error
		if folder, err = domain.ImageFolder(p.SKU); err != nil {
			return err // unreachable: a generated SKU is always a safe folder name
		}
	}

	if imgs.Main == nil {
		v.Add("main_image", "A imagem principal é obrigatória")
	}
	files, ferr := prepareFiles(imgs, nil)
	mergeValidation(v, ferr)
	if err := v.OrNil(); err != nil {
		return err
	}

	p.CreatedBy = actor.ID
	p.IsActive = true
	var written []string
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.products.Create(ctx, p); err != nil {
			return conflictToValidation(err, productConflicts)
		}
		return s.storeImages(ctx, actor, p.ID, folder, files, false, &written)
	})
	if err != nil {
		s.removeFiles(ctx, folder, written)
		return imageConflictError(err)
	}
	return nil
}

// activeBrandName returns the name of an existing, active brand, or a validation error on brand_id.
func (s *CatalogService) activeBrandName(ctx context.Context, id int) (string, error) {
	b, err := s.brands.FindByID(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return "", invalidField("brand_id", "Marca não encontrada")
	}
	if err != nil {
		return "", err
	}
	if !b.IsActive {
		return "", invalidField("brand_id", "Marca inativa")
	}
	return b.Name, nil
}

func (s *CatalogService) UpdateProduct(ctx context.Context, p *domain.Product) error {
	p.Normalize()
	current, err := s.products.FindByID(ctx, p.ID)
	if err != nil {
		return err
	}
	// The SKU is generated at creation and names the image folder: it is never edited.
	p.SKU = current.SKU
	if err := p.Validate(); err != nil {
		return err
	}
	if p.BrandID > 0 && p.BrandID != current.BrandID {
		if _, err := s.activeBrandName(ctx, p.BrandID); err != nil {
			return err
		}
	}
	return conflictToValidation(s.products.Update(ctx, p), productConflicts)
}

func (s *CatalogService) SetProductActive(ctx context.Context, id int, active bool) error {
	return s.products.SetActive(ctx, id, active)
}

func (s *CatalogService) ListPrices(ctx context.Context, productID int) ([]domain.Pricebook, error) {
	return s.prices.ListByProduct(ctx, productID)
}

func (s *CatalogService) AddPrice(ctx context.Context, actor *domain.User, p *domain.Pricebook) error {
	p.ValidFrom = dateOf(p.ValidFrom)
	if p.ValidTo != nil {
		to := dateOf(*p.ValidTo)
		p.ValidTo = &to
	}
	// Validate derives the markup from the informed final price.
	if err := p.Validate(); err != nil {
		return err
	}
	if _, err := s.products.FindByID(ctx, p.ProductID); err != nil {
		return err
	}
	p.CreatedBy = actor.ID
	return s.prices.Create(ctx, p)
}
