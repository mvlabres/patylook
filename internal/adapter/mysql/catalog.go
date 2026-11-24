package mysql

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// ---- product categories ----

type ProductCategoryRepository struct{ base }

var _ port.ProductCategoryRepository = (*ProductCategoryRepository)(nil)

func NewProductCategoryRepository(db *sql.DB) *ProductCategoryRepository {
	return &ProductCategoryRepository{base{db}}
}

func (r *ProductCategoryRepository) List(ctx context.Context) ([]domain.ProductCategory, error) {
	rows, err := r.q(ctx).QueryContext(ctx, "SELECT id, name, description FROM product_category ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ProductCategory
	for rows.Next() {
		var c domain.ProductCategory
		if err := rows.Scan(&c.ID, &c.Name, &c.Description); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *ProductCategoryRepository) FindByID(ctx context.Context, id int) (*domain.ProductCategory, error) {
	var c domain.ProductCategory
	err := r.q(ctx).QueryRowContext(ctx, "SELECT id, name, description FROM product_category WHERE id = ?", id).
		Scan(&c.ID, &c.Name, &c.Description)
	if err != nil {
		return nil, translate(err)
	}
	return &c, nil
}

func (r *ProductCategoryRepository) Create(ctx context.Context, c *domain.ProductCategory) error {
	res, err := r.q(ctx).ExecContext(ctx, "INSERT INTO product_category (name, description) VALUES (?, ?)", c.Name, c.Description)
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	c.ID = int(id)
	return err
}

func (r *ProductCategoryRepository) Update(ctx context.Context, c *domain.ProductCategory) error {
	_, err := r.q(ctx).ExecContext(ctx, "UPDATE product_category SET name = ?, description = ? WHERE id = ?", c.Name, c.Description, c.ID)
	return translate(err)
}

func (r *ProductCategoryRepository) Delete(ctx context.Context, id int) error {
	_, err := r.q(ctx).ExecContext(ctx, "DELETE FROM product_category WHERE id = ?", id)
	return translate(err)
}

// ---- products ----

type ProductRepository struct {
	base
	loc *time.Location
}

var _ port.ProductRepository = (*ProductRepository)(nil)

func NewProductRepository(db *sql.DB, loc *time.Location) *ProductRepository {
	return &ProductRepository{base: base{db}, loc: loc}
}

// productSelect projects the price valid on a date (two date placeholders come first).
// Joined names are aliased because ListSellable wraps this query in a derived table, where
// duplicate column names are an error.
const productSelect = "SELECT p.id, p.name, COALESCE(p.description, ''), p.sku, p.product_category_id, pc.name AS category_name, " +
	"p.brand_id, b.name AS brand_name, " +
	"p.stock, p.is_active, p.created_by, p.created_at, " +
	"(SELECT pb.final_price FROM pricebook pb WHERE pb.product_id = p.id AND pb.valid_from <= ? " +
	"AND (pb.valid_to IS NULL OR pb.valid_to >= ?) ORDER BY pb.valid_from DESC, pb.id DESC LIMIT 1) AS current_price " +
	"FROM product p JOIN product_category pc ON pc.id = p.product_category_id " +
	"LEFT JOIN brand b ON b.id = p.brand_id "

func scanProduct(s scanner) (*domain.Product, error) {
	var (
		p         domain.Product
		brandID   sql.NullInt64
		brandName sql.NullString
		created   sql.NullTime
	)
	err := s.Scan(&p.ID, &p.Name, &p.Description, &p.SKU, &p.CategoryID, &p.CategoryName, &brandID, &brandName,
		&p.Stock, &p.IsActive, &p.CreatedBy, &created, nullMoneyCol{&p.CurrentPrice})
	if err != nil {
		return nil, err
	}
	p.BrandID, p.BrandName = int(brandID.Int64), brandName.String
	p.CreatedAt = timeOrZero(created)
	return &p, nil
}

func (r *ProductRepository) FindByID(ctx context.Context, id int) (*domain.Product, error) {
	day := dateParam(time.Now().In(r.loc))
	p, err := scanProduct(r.q(ctx).QueryRowContext(ctx, productSelect+"WHERE p.id = ?", day, day, id))
	return p, translate(err)
}

func (r *ProductRepository) List(ctx context.Context, lp port.ListParams, on time.Time) ([]domain.Product, int, error) {
	where, args := "", []any{}
	var conds []string
	if lp.Search != "" {
		like := likePattern(lp.Search)
		conds = append(conds, "(p.name LIKE ? OR p.sku LIKE ?)")
		args = append(args, like, like)
	}
	if lp.OnlyActive {
		conds = append(conds, "p.is_active = TRUE")
	}
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ") + " "
	}
	var total int
	if err := r.q(ctx).QueryRowContext(ctx, "SELECT COUNT(*) FROM product p "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	day := dateParam(on)
	all := append([]any{day, day}, args...)
	rows, err := r.q(ctx).QueryContext(ctx, productSelect+where+"ORDER BY p.name LIMIT ? OFFSET ?",
		append(all, lp.Limit, lp.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *p)
	}
	return out, total, rows.Err()
}

func (r *ProductRepository) ListSellable(ctx context.Context, on time.Time) ([]domain.Product, error) {
	day := dateParam(on)
	query := "SELECT * FROM (" + productSelect + "WHERE p.is_active = TRUE AND p.stock > 0) t WHERE current_price IS NOT NULL ORDER BY name"
	rows, err := r.q(ctx).QueryContext(ctx, query, day, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *ProductRepository) Create(ctx context.Context, p *domain.Product) error {
	res, err := r.q(ctx).ExecContext(ctx,
		"INSERT INTO product (name, description, sku, product_category_id, brand_id, stock, is_active, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		p.Name, nullString(p.Description), p.SKU, p.CategoryID, nullInt(p.BrandID), p.Stock, p.IsActive, p.CreatedBy)
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	p.ID = int(id)
	return err
}

func (r *ProductRepository) Update(ctx context.Context, p *domain.Product) error {
	// The SKU is deliberately absent: it is set once, at creation.
	_, err := r.q(ctx).ExecContext(ctx,
		"UPDATE product SET name = ?, description = ?, product_category_id = ?, brand_id = ?, stock = ? WHERE id = ?",
		p.Name, nullString(p.Description), p.CategoryID, nullInt(p.BrandID), p.Stock, p.ID)
	return translate(err)
}

func (r *ProductRepository) SetActive(ctx context.Context, id int, active bool) error {
	_, err := r.q(ctx).ExecContext(ctx, "UPDATE product SET is_active = ? WHERE id = ?", active, id)
	return translate(err)
}

func (r *ProductRepository) DecrementStock(ctx context.Context, id, qty int) error {
	res, err := r.q(ctx).ExecContext(ctx, "UPDATE product SET stock = stock - ? WHERE id = ? AND stock >= ?", qty, id, qty)
	if err != nil {
		return translate(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return domain.ErrInsufficientStock
	}
	return nil
}

func (r *ProductRepository) IncrementStock(ctx context.Context, id, qty int) error {
	_, err := r.q(ctx).ExecContext(ctx, "UPDATE product SET stock = stock + ? WHERE id = ?", qty, id)
	return translate(err)
}

func (r *ProductRepository) CountLowStock(ctx context.Context, threshold int) (int, error) {
	var n int
	err := r.q(ctx).QueryRowContext(ctx, "SELECT COUNT(*) FROM product WHERE is_active = TRUE AND stock <= ?", threshold).Scan(&n)
	return n, err
}

// ---- pricebook ----

type PricebookRepository struct{ base }

var _ port.PricebookRepository = (*PricebookRepository)(nil)

func NewPricebookRepository(db *sql.DB) *PricebookRepository { return &PricebookRepository{base{db}} }

const pricebookSelect = "SELECT pb.id, pb.product_id, pb.product_cost, pb.markup_percent, pb.packaging_cost, pb.logistics_cost, " +
	"pb.other_costs, pb.final_price, pb.valid_from, pb.valid_to, pb.created_by, u.name, pb.created_at " +
	"FROM pricebook pb JOIN `user` u ON u.id = pb.created_by "

func scanPricebook(s scanner) (*domain.Pricebook, error) {
	var (
		p       domain.Pricebook
		to      sql.NullTime
		created sql.NullTime
	)
	err := s.Scan(&p.ID, &p.ProductID, moneyCol{&p.ProductCost}, percentCol{&p.MarkupPercent},
		moneyCol{&p.PackagingCost}, moneyCol{&p.LogisticsCost}, moneyCol{&p.OtherCosts}, moneyCol{&p.FinalPrice},
		&p.ValidFrom, &to, &p.CreatedBy, &p.CreatedByName, &created)
	if err != nil {
		return nil, err
	}
	if to.Valid {
		p.ValidTo = &to.Time
	}
	p.CreatedAt = timeOrZero(created)
	return &p, nil
}

func (r *PricebookRepository) ListByProduct(ctx context.Context, productID int) ([]domain.Pricebook, error) {
	rows, err := r.q(ctx).QueryContext(ctx, pricebookSelect+"WHERE pb.product_id = ? ORDER BY pb.valid_from DESC, pb.id DESC", productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Pricebook
	for rows.Next() {
		p, err := scanPricebook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *PricebookRepository) FindCurrent(ctx context.Context, productID int, on time.Time) (*domain.Pricebook, error) {
	day := dateParam(on)
	p, err := scanPricebook(r.q(ctx).QueryRowContext(ctx,
		pricebookSelect+"WHERE pb.product_id = ? AND pb.valid_from <= ? AND (pb.valid_to IS NULL OR pb.valid_to >= ?) "+
			"ORDER BY pb.valid_from DESC, pb.id DESC LIMIT 1", productID, day, day))
	return p, translate(err)
}

func (r *PricebookRepository) Create(ctx context.Context, p *domain.Pricebook) error {
	res, err := r.q(ctx).ExecContext(ctx,
		"INSERT INTO pricebook (product_id, product_cost, markup_percent, valid_from, valid_to, final_price, "+
			"logistics_cost, other_costs, packaging_cost, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		p.ProductID, p.ProductCost.Decimal(), p.MarkupPercent.Decimal(), dateParam(p.ValidFrom), nullDate(p.ValidTo),
		p.FinalPrice.Decimal(), p.LogisticsCost.Decimal(), p.OtherCosts.Decimal(), p.PackagingCost.Decimal(), p.CreatedBy)
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	p.ID = int(id)
	return err
}
