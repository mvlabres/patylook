package mysql

import (
	"context"
	"database/sql"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type BrandRepository struct{ base }

var _ port.BrandRepository = (*BrandRepository)(nil)

func NewBrandRepository(db *sql.DB) *BrandRepository { return &BrandRepository{base{db}} }

const brandSelect = "SELECT b.id, b.name, b.is_active, b.created_by, u.name, b.created_at " +
	"FROM brand b JOIN `user` u ON u.id = b.created_by "

func scanBrand(s scanner) (*domain.Brand, error) {
	var (
		b       domain.Brand
		created sql.NullTime
	)
	if err := s.Scan(&b.ID, &b.Name, &b.IsActive, &b.CreatedBy, &b.CreatedByName, &created); err != nil {
		return nil, err
	}
	b.CreatedAt = timeOrZero(created)
	return &b, nil
}

func (r *BrandRepository) FindByID(ctx context.Context, id int) (*domain.Brand, error) {
	b, err := scanBrand(r.q(ctx).QueryRowContext(ctx, brandSelect+"WHERE b.id = ?", id))
	return b, translate(err)
}

func (r *BrandRepository) List(ctx context.Context, p port.ListParams) ([]domain.Brand, int, error) {
	where, args := "", []any{}
	if p.Search != "" {
		where = "WHERE b.name LIKE ? "
		args = append(args, likePattern(p.Search))
	}
	var total int
	if err := r.q(ctx).QueryRowContext(ctx, "SELECT COUNT(*) FROM brand b "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := r.list(ctx, brandSelect+where+"ORDER BY b.name LIMIT ? OFFSET ?", append(args, p.Limit, p.Offset)...)
	return items, total, err
}

func (r *BrandRepository) ListActive(ctx context.Context) ([]domain.Brand, error) {
	return r.list(ctx, brandSelect+"WHERE b.is_active = TRUE ORDER BY b.name")
}

func (r *BrandRepository) list(ctx context.Context, query string, args ...any) ([]domain.Brand, error) {
	rows, err := r.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Brand
	for rows.Next() {
		b, err := scanBrand(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (r *BrandRepository) Create(ctx context.Context, b *domain.Brand) error {
	res, err := r.q(ctx).ExecContext(ctx,
		"INSERT INTO brand (name, is_active, created_by) VALUES (?, ?, ?)", b.Name, b.IsActive, b.CreatedBy)
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	b.ID = int(id)
	return err
}

func (r *BrandRepository) Update(ctx context.Context, b *domain.Brand) error {
	_, err := r.q(ctx).ExecContext(ctx, "UPDATE brand SET name = ? WHERE id = ?", b.Name, b.ID)
	return translate(err)
}

func (r *BrandRepository) SetActive(ctx context.Context, id int, active bool) error {
	_, err := r.q(ctx).ExecContext(ctx, "UPDATE brand SET is_active = ? WHERE id = ?", active, id)
	return translate(err)
}
