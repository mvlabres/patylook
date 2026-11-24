package service

import (
	"context"
	"log/slog"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type CatalogService struct {
	categories port.ProductCategoryRepository
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
	c port.ProductCategoryRepository, p port.ProductRepository, pb port.PricebookRepository,
	images port.ProductImageRepository, storage port.ImageStorage, tx port.Transactor, now Clock, log *slog.Logger,
) *CatalogService {
	return &CatalogService{categories: c, products: p, prices: pb, images: images, storage: storage, tx: tx, now: now, log: log}
}

var (
	categoryConflicts = map[string]string{"name": "Já existe uma categoria com esse nome"}
	productConflicts  = map[string]string{"sku": "Já existe um produto com esse SKU"}
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
	v := domain.NewValidationError()
	if err := p.Validate(); err != nil {
		mergeValidation(v, err)
	}
	folder, err := domain.ImageFolder(p.SKU)
	if err != nil && p.SKU != "" {
		v.Add("sku", "SKU deve conter ao menos uma letra ou número, pois nomeia a pasta de imagens")
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
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
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

func (s *CatalogService) UpdateProduct(ctx context.Context, p *domain.Product) error {
	p.Normalize()
	if err := p.Validate(); err != nil {
		return err
	}
	current, err := s.products.FindByID(ctx, p.ID)
	if err != nil {
		return err
	}
	if p.SKU != current.SKU {
		// The SKU names the image folder, so it is locked while images exist.
		imgs, err := s.images.ListByProduct(ctx, p.ID)
		if err != nil {
			return err
		}
		if len(imgs) > 0 {
			return invalidField("sku", "O SKU não pode ser alterado enquanto o produto tiver imagens (a pasta de imagens usa o SKU)")
		}
		if _, err := domain.ImageFolder(p.SKU); err != nil {
			return invalidField("sku", "SKU deve conter ao menos uma letra ou número, pois nomeia a pasta de imagens")
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
	if err := p.Validate(); err != nil {
		return err
	}
	if _, err := s.products.FindByID(ctx, p.ProductID); err != nil {
		return err
	}
	p.ComputeFinalPrice()
	p.CreatedBy = actor.ID
	return s.prices.Create(ctx, p)
}
