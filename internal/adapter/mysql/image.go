package mysql

import (
	"context"
	"database/sql"
	"strings"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type ProductImageRepository struct{ base }

var _ port.ProductImageRepository = (*ProductImageRepository)(nil)

func NewProductImageRepository(db *sql.DB) *ProductImageRepository {
	return &ProductImageRepository{base{db}}
}

// Main image first, then carousel order.
const imageSelect = "SELECT id, product_id, file_name, path, sort_order, created_by, created_at FROM product_images "
const imageOrder = " ORDER BY (file_name = '" + domain.MainImageName + "') DESC, sort_order, id"

func scanImage(s scanner) (*domain.ProductImage, error) {
	var (
		i       domain.ProductImage
		created sql.NullTime
	)
	if err := s.Scan(&i.ID, &i.ProductID, &i.FileName, &i.Path, &i.SortOrder, &i.CreatedBy, &created); err != nil {
		return nil, err
	}
	i.CreatedAt = timeOrZero(created)
	return &i, nil
}

func (r *ProductImageRepository) collect(ctx context.Context, query string, args ...any) ([]domain.ProductImage, error) {
	rows, err := r.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ProductImage
	for rows.Next() {
		i, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (r *ProductImageRepository) ListByProduct(ctx context.Context, productID int) ([]domain.ProductImage, error) {
	return r.collect(ctx, imageSelect+"WHERE product_id = ?"+imageOrder, productID)
}

func (r *ProductImageRepository) ListByProducts(ctx context.Context, ids []int) (map[int][]domain.ProductImage, error) {
	out := make(map[int][]domain.ProductImage, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	list, err := r.collect(ctx, imageSelect+"WHERE product_id IN ("+marks+")"+imageOrder, args...)
	if err != nil {
		return nil, err
	}
	for _, img := range list {
		out[img.ProductID] = append(out[img.ProductID], img)
	}
	return out, nil
}

func (r *ProductImageRepository) FindByID(ctx context.Context, id int) (*domain.ProductImage, error) {
	i, err := scanImage(r.q(ctx).QueryRowContext(ctx, imageSelect+"WHERE id = ?", id))
	return i, translate(err)
}

func (r *ProductImageRepository) Create(ctx context.Context, img *domain.ProductImage) error {
	res, err := r.q(ctx).ExecContext(ctx,
		"INSERT INTO product_images (product_id, file_name, path, sort_order, created_by) VALUES (?, ?, ?, ?, ?)",
		img.ProductID, img.FileName, img.Path, img.SortOrder, img.CreatedBy)
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	img.ID = int(id)
	return err
}

func (r *ProductImageRepository) Delete(ctx context.Context, id int) error {
	_, err := r.q(ctx).ExecContext(ctx, "DELETE FROM product_images WHERE id = ?", id)
	return translate(err)
}
