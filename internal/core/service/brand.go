package service

import (
	"context"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type BrandService struct {
	brands port.BrandRepository
}

var _ port.BrandService = (*BrandService)(nil)

func NewBrandService(b port.BrandRepository) *BrandService {
	return &BrandService{brands: b}
}

var brandConflicts = map[string]string{"name": "Já existe uma marca com esse nome"}

func (s *BrandService) List(ctx context.Context, page int, search string) (port.Page[domain.Brand], error) {
	page, lp := listParams(page, search)
	items, total, err := s.brands.List(ctx, lp)
	if err != nil {
		return port.Page[domain.Brand]{}, err
	}
	return newPage(items, total, page), nil
}

func (s *BrandService) ListActive(ctx context.Context) ([]domain.Brand, error) {
	return s.brands.ListActive(ctx)
}

func (s *BrandService) Get(ctx context.Context, id int) (*domain.Brand, error) {
	return s.brands.FindByID(ctx, id)
}

func (s *BrandService) Save(ctx context.Context, actor *domain.User, b *domain.Brand) error {
	b.Normalize()
	if err := b.Validate(); err != nil {
		return err
	}
	var err error
	if b.ID == 0 {
		b.IsActive = true
		b.CreatedBy = actor.ID
		err = s.brands.Create(ctx, b)
	} else {
		err = s.brands.Update(ctx, b)
	}
	return conflictToValidation(err, brandConflicts)
}

func (s *BrandService) SetActive(ctx context.Context, id int, active bool) error {
	return s.brands.SetActive(ctx, id, active)
}
