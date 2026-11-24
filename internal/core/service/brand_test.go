package service

import (
	"context"
	"errors"
	"testing"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type memBrands struct {
	port.BrandRepository
	created *domain.Brand
	updated *domain.Brand
	err     error
}

func (m *memBrands) Create(_ context.Context, b *domain.Brand) error {
	if m.err != nil {
		return m.err
	}
	c := *b
	m.created = &c
	return nil
}

func (m *memBrands) Update(_ context.Context, b *domain.Brand) error {
	c := *b
	m.updated = &c
	return m.err
}

func fieldError(t *testing.T, err error, field string) {
	t.Helper()
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("erro = %v, quer ValidationError", err)
	}
	if _, ok := ve.Fields[field]; !ok {
		t.Errorf("sem erro no campo %q: %v", field, ve.Fields)
	}
}

func TestBrandSaveCreateRecordsActorAndActivates(t *testing.T) {
	repo := &memBrands{}
	svc := NewBrandService(repo)

	b := &domain.Brand{Name: "  Natura  "}
	if err := svc.Save(context.Background(), &domain.User{ID: 7}, b); err != nil {
		t.Fatal(err)
	}
	if repo.created == nil || repo.created.Name != "Natura" || repo.created.CreatedBy != 7 || !repo.created.IsActive {
		t.Errorf("marca criada = %+v", repo.created)
	}
}

func TestBrandSaveUpdateKeepsCreator(t *testing.T) {
	repo := &memBrands{}
	svc := NewBrandService(repo)

	if err := svc.Save(context.Background(), &domain.User{ID: 7}, &domain.Brand{ID: 3, Name: "Avon"}); err != nil {
		t.Fatal(err)
	}
	if repo.created != nil || repo.updated == nil || repo.updated.ID != 3 || repo.updated.CreatedBy != 0 {
		t.Errorf("created=%+v updated=%+v", repo.created, repo.updated)
	}
}

func TestBrandSaveValidation(t *testing.T) {
	svc := NewBrandService(&memBrands{})
	actor := &domain.User{ID: 1}

	fieldError(t, svc.Save(context.Background(), actor, &domain.Brand{Name: "   "}), "name")
	long := make([]byte, 101)
	for i := range long {
		long[i] = 'a'
	}
	fieldError(t, svc.Save(context.Background(), actor, &domain.Brand{Name: string(long)}), "name")
}

func TestBrandSaveDuplicateNameBecomesFieldError(t *testing.T) {
	svc := NewBrandService(&memBrands{err: &domain.ConflictError{Field: "name"}})
	err := svc.Save(context.Background(), &domain.User{ID: 1}, &domain.Brand{Name: "Natura"})
	fieldError(t, err, "name")
}
