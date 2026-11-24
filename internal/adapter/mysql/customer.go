package mysql

import (
	"context"
	"database/sql"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// ---- customers ----

type CustomerRepository struct{ base }

var _ port.CustomerRepository = (*CustomerRepository)(nil)

func NewCustomerRepository(db *sql.DB) *CustomerRepository { return &CustomerRepository{base{db}} }

const customerSelect = "SELECT id, name, document, phone, email, is_active, created_at FROM customer "

func scanCustomer(s scanner) (*domain.Customer, error) {
	var (
		c       domain.Customer
		created sql.NullTime
	)
	if err := s.Scan(&c.ID, &c.Name, &c.Document, &c.Phone, &c.Email, &c.IsActive, &created); err != nil {
		return nil, err
	}
	c.CreatedAt = timeOrZero(created)
	return &c, nil
}

func (r *CustomerRepository) FindByID(ctx context.Context, id int) (*domain.Customer, error) {
	c, err := scanCustomer(r.q(ctx).QueryRowContext(ctx, customerSelect+"WHERE id = ?", id))
	return c, translate(err)
}

func (r *CustomerRepository) list(ctx context.Context, query string, args ...any) ([]domain.Customer, error) {
	rows, err := r.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Customer
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *CustomerRepository) List(ctx context.Context, p port.ListParams) ([]domain.Customer, int, error) {
	where, args := "", []any{}
	if p.Search != "" {
		like := likePattern(p.Search)
		where = "WHERE (name LIKE ? OR document LIKE ? OR email LIKE ? OR phone LIKE ?) "
		args = append(args, like, like, like, like)
	}
	var total int
	if err := r.q(ctx).QueryRowContext(ctx, "SELECT COUNT(*) FROM customer "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := r.list(ctx, customerSelect+where+"ORDER BY name LIMIT ? OFFSET ?", append(args, p.Limit, p.Offset)...)
	return items, total, err
}

func (r *CustomerRepository) ListActive(ctx context.Context) ([]domain.Customer, error) {
	return r.list(ctx, customerSelect+"WHERE is_active = TRUE ORDER BY name")
}

func (r *CustomerRepository) Create(ctx context.Context, c *domain.Customer) error {
	res, err := r.q(ctx).ExecContext(ctx,
		"INSERT INTO customer (name, document, phone, email, is_active) VALUES (?, ?, ?, ?, ?)",
		c.Name, c.Document, c.Phone, c.Email, c.IsActive)
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	c.ID = int(id)
	return err
}

func (r *CustomerRepository) Update(ctx context.Context, c *domain.Customer) error {
	_, err := r.q(ctx).ExecContext(ctx,
		"UPDATE customer SET name = ?, document = ?, phone = ?, email = ? WHERE id = ?",
		c.Name, c.Document, c.Phone, c.Email, c.ID)
	return translate(err)
}

func (r *CustomerRepository) SetActive(ctx context.Context, id int, active bool) error {
	_, err := r.q(ctx).ExecContext(ctx, "UPDATE customer SET is_active = ? WHERE id = ?", active, id)
	return translate(err)
}

// ---- payment types ----

type PaymentTypeRepository struct{ base }

var _ port.PaymentTypeRepository = (*PaymentTypeRepository)(nil)

func NewPaymentTypeRepository(db *sql.DB) *PaymentTypeRepository {
	return &PaymentTypeRepository{base{db}}
}

const paymentTypeSelect = "SELECT id, name, description, surcharge_percent FROM payment_type "

func scanPaymentType(s scanner) (*domain.PaymentType, error) {
	var p domain.PaymentType
	if err := s.Scan(&p.ID, &p.Name, &p.Description, percentCol{&p.SurchargePercent}); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PaymentTypeRepository) List(ctx context.Context) ([]domain.PaymentType, error) {
	rows, err := r.q(ctx).QueryContext(ctx, paymentTypeSelect+"ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PaymentType
	for rows.Next() {
		p, err := scanPaymentType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *PaymentTypeRepository) FindByID(ctx context.Context, id int) (*domain.PaymentType, error) {
	p, err := scanPaymentType(r.q(ctx).QueryRowContext(ctx, paymentTypeSelect+"WHERE id = ?", id))
	return p, translate(err)
}

func (r *PaymentTypeRepository) Create(ctx context.Context, p *domain.PaymentType) error {
	res, err := r.q(ctx).ExecContext(ctx,
		"INSERT INTO payment_type (name, description, surcharge_percent) VALUES (?, ?, ?)",
		p.Name, p.Description, p.SurchargePercent.Decimal())
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	p.ID = int(id)
	return err
}

func (r *PaymentTypeRepository) Update(ctx context.Context, p *domain.PaymentType) error {
	_, err := r.q(ctx).ExecContext(ctx,
		"UPDATE payment_type SET name = ?, description = ?, surcharge_percent = ? WHERE id = ?",
		p.Name, p.Description, p.SurchargePercent.Decimal(), p.ID)
	return translate(err)
}

func (r *PaymentTypeRepository) Delete(ctx context.Context, id int) error {
	_, err := r.q(ctx).ExecContext(ctx, "DELETE FROM payment_type WHERE id = ?", id)
	return translate(err)
}
