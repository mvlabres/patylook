package mysql

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type OrderRepository struct{ base }

var _ port.OrderRepository = (*OrderRepository)(nil)

func NewOrderRepository(db *sql.DB) *OrderRepository { return &OrderRepository{base{db}} }

const orderSelect = "SELECT o.id, o.user_id, o.customer_id, o.payment_type_id, o.status, o.subtotal, o.discount, " +
	"o.payment_addition, o.total, COALESCE(o.notes, ''), o.created_at, u.name, c.name, pt.name " +
	"FROM orders o JOIN `user` u ON u.id = o.user_id JOIN customer c ON c.id = o.customer_id " +
	"JOIN payment_type pt ON pt.id = o.payment_type_id "

func scanOrder(s scanner) (*domain.Order, error) {
	var (
		o       domain.Order
		status  string
		created sql.NullTime
	)
	err := s.Scan(&o.ID, &o.UserID, &o.CustomerID, &o.PaymentTypeID, &status, moneyCol{&o.Subtotal},
		moneyCol{&o.Discount}, moneyCol{&o.PaymentAddition}, moneyCol{&o.Total}, &o.Notes, &created,
		&o.UserName, &o.CustomerName, &o.PaymentTypeName)
	if err != nil {
		return nil, err
	}
	o.Status = domain.OrderStatus(status)
	o.CreatedAt = timeOrZero(created)
	return &o, nil
}

func (r *OrderRepository) Create(ctx context.Context, o *domain.Order) error {
	res, err := r.q(ctx).ExecContext(ctx,
		"INSERT INTO orders (user_id, customer_id, payment_type_id, status, subtotal, discount, payment_addition, total, notes) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		o.UserID, o.CustomerID, o.PaymentTypeID, string(o.Status), o.Subtotal.Decimal(), o.Discount.Decimal(),
		o.PaymentAddition.Decimal(), o.Total.Decimal(), nullString(o.Notes))
	if err != nil {
		return translate(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	o.ID = int(id)

	for _, it := range o.Items {
		var pb any
		if it.PricebookID != nil {
			pb = *it.PricebookID
		}
		_, err := r.q(ctx).ExecContext(ctx,
			"INSERT INTO order_item (order_id, product_id, quantity, unit_price, unit_cost, discount, pricebook_id) VALUES (?, ?, ?, ?, ?, ?, ?)",
			o.ID, it.ProductID, it.Quantity, it.UnitPrice.Decimal(), it.UnitCost.Decimal(), it.Discount.Decimal(), pb)
		if err != nil {
			return translate(err)
		}
	}
	return nil
}

func (r *OrderRepository) FindByID(ctx context.Context, id int) (*domain.Order, error) {
	o, err := scanOrder(r.q(ctx).QueryRowContext(ctx, orderSelect+"WHERE o.id = ?", id))
	if err != nil {
		return nil, translate(err)
	}
	rows, err := r.q(ctx).QueryContext(ctx,
		"SELECT oi.product_id, oi.quantity, oi.unit_price, oi.unit_cost, oi.discount, oi.pricebook_id, p.name, p.sku "+
			"FROM order_item oi JOIN product p ON p.id = oi.product_id WHERE oi.order_id = ? ORDER BY p.name", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			it domain.OrderItem
			pb sql.NullInt64
		)
		if err := rows.Scan(&it.ProductID, &it.Quantity, moneyCol{&it.UnitPrice}, moneyCol{&it.UnitCost},
			moneyCol{&it.Discount}, &pb, &it.ProductName, &it.ProductSKU); err != nil {
			return nil, err
		}
		if pb.Valid {
			v := int(pb.Int64)
			it.PricebookID = &v
		}
		o.Items = append(o.Items, it)
	}
	return o, rows.Err()
}

func (r *OrderRepository) List(ctx context.Context, f port.OrderFilter) ([]domain.Order, int, error) {
	var (
		conds []string
		args  []any
	)
	if f.Status != "" {
		conds = append(conds, "o.status = ?")
		args = append(args, string(f.Status))
	}
	if f.Search != "" {
		id, _ := strconv.Atoi(strings.TrimPrefix(f.Search, "#"))
		conds = append(conds, "(c.name LIKE ? OR o.id = ?)")
		args = append(args, likePattern(f.Search), id)
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ") + " "
	}

	var total int
	err := r.q(ctx).QueryRowContext(ctx,
		"SELECT COUNT(*) FROM orders o JOIN customer c ON c.id = o.customer_id "+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	orders, err := r.query(ctx, orderSelect+where+"ORDER BY o.id DESC LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	return orders, total, err
}

func (r *OrderRepository) Recent(ctx context.Context, limit int) ([]domain.Order, error) {
	return r.query(ctx, orderSelect+"ORDER BY o.id DESC LIMIT ?", limit)
}

func (r *OrderRepository) query(ctx context.Context, query string, args ...any) ([]domain.Order, error) {
	rows, err := r.q(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

func (r *OrderRepository) UpdateStatus(ctx context.Context, id int, from, to domain.OrderStatus) error {
	res, err := r.q(ctx).ExecContext(ctx, "UPDATE orders SET status = ? WHERE id = ? AND status = ?", string(to), id, string(from))
	if err != nil {
		return translate(err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return domain.ErrConflict
	}
	return nil
}

func (r *OrderRepository) Summary(ctx context.Context, since time.Time) (port.OrderSummary, error) {
	var s port.OrderSummary
	err := r.q(ctx).QueryRowContext(ctx,
		"SELECT COALESCE(SUM(status = 'open'), 0), COALESCE(SUM(status = 'completed'), 0), "+
			"COALESCE(SUM(CASE WHEN status = 'completed' THEN total ELSE 0 END), 0) FROM orders WHERE created_at >= ?", since).
		Scan(&s.OpenOrders, &s.CompletedOrders, moneyCol{&s.CompletedTotal})
	return s, err
}
