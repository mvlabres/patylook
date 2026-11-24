package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type OrderService struct {
	orders    port.OrderRepository
	products  port.ProductRepository
	prices    port.PricebookRepository
	customers port.CustomerRepository
	payments  port.PaymentTypeRepository
	tx        port.Transactor
	now       Clock
}

var _ port.OrderService = (*OrderService)(nil)

func NewOrderService(
	orders port.OrderRepository, products port.ProductRepository, prices port.PricebookRepository,
	customers port.CustomerRepository, payments port.PaymentTypeRepository, tx port.Transactor, now Clock,
) *OrderService {
	return &OrderService{orders: orders, products: products, prices: prices, customers: customers, payments: payments, tx: tx, now: now}
}

func (s *OrderService) FormData(ctx context.Context) (*port.OrderFormData, error) {
	customers, err := s.customers.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	payments, err := s.payments.List(ctx)
	if err != nil {
		return nil, err
	}
	products, err := s.products.ListSellable(ctx, dateOf(s.now()))
	if err != nil {
		return nil, err
	}
	return &port.OrderFormData{Customers: customers, PaymentTypes: payments, Products: products}, nil
}

func (s *OrderService) Create(ctx context.Context, actor *domain.User, in port.CreateOrderInput) (*domain.Order, error) {
	v := domain.NewValidationError()
	order := &domain.Order{
		UserID:        actor.ID,
		CustomerID:    in.CustomerID,
		PaymentTypeID: in.PaymentTypeID,
		Status:        domain.OrderOpen,
		Discount:      in.Discount,
		Notes:         in.Notes,
	}
	order.Normalize()

	customer, err := s.customers.FindByID(ctx, in.CustomerID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		v.Add("customer_id", "Cliente não encontrado")
	case err != nil:
		return nil, err
	case !customer.IsActive:
		v.Add("customer_id", "Cliente inativo")
	}

	payment, err := s.payments.FindByID(ctx, in.PaymentTypeID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		v.Add("payment_type_id", "Forma de pagamento não encontrada")
	case err != nil:
		return nil, err
	}

	today := dateOf(s.now())
	for _, item := range in.Items {
		oi, err := s.buildItem(ctx, item, today)
		if err != nil {
			var ve *domain.ValidationError
			if errors.As(err, &ve) {
				mergeValidation(v, err)
				continue
			}
			return nil, err
		}
		order.Items = append(order.Items, oi)
	}
	if err := v.OrNil(); err != nil {
		return nil, err
	}

	if err := order.Calculate(payment.SurchargePercent); err != nil {
		return nil, err
	}

	// Lock stock rows in a stable order so concurrent orders cannot deadlock.
	sort.Slice(order.Items, func(i, j int) bool { return order.Items[i].ProductID < order.Items[j].ProductID })

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.orders.Create(ctx, order); err != nil {
			return err
		}
		for _, it := range order.Items {
			if err := s.products.DecrementStock(ctx, it.ProductID, it.Quantity); err != nil {
				if errors.Is(err, domain.ErrInsufficientStock) {
					ve := domain.NewValidationError()
					ve.Add("items", fmt.Sprintf("Estoque insuficiente para %s", it.ProductName))
					return ve
				}
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return order, nil
}

// buildItem snapshots the current price and cost of a product into an order item.
func (s *OrderService) buildItem(ctx context.Context, in port.OrderItemInput, today time.Time) (domain.OrderItem, error) {
	product, err := s.products.FindByID(ctx, in.ProductID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.OrderItem{}, itemError("Produto não encontrado")
	}
	if err != nil {
		return domain.OrderItem{}, err
	}
	if !product.IsActive {
		return domain.OrderItem{}, itemError(fmt.Sprintf("Produto %s está inativo", product.Name))
	}
	price, err := s.prices.FindCurrent(ctx, product.ID, today)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.OrderItem{}, itemError(fmt.Sprintf("Produto %s não possui preço vigente", product.Name))
	}
	if err != nil {
		return domain.OrderItem{}, err
	}
	pbID := price.ID
	return domain.OrderItem{
		ProductID:   product.ID,
		Quantity:    in.Quantity,
		UnitPrice:   price.FinalPrice,
		UnitCost:    price.TotalCost(),
		Discount:    in.Discount,
		PricebookID: &pbID,
		ProductName: product.Name,
		ProductSKU:  product.SKU,
	}, nil
}

func itemError(msg string) error {
	v := domain.NewValidationError()
	v.Add("items", msg)
	return v
}

func (s *OrderService) Get(ctx context.Context, id int) (*domain.Order, error) {
	return s.orders.FindByID(ctx, id)
}

func (s *OrderService) List(ctx context.Context, page int, status domain.OrderStatus, search string) (port.Page[domain.Order], error) {
	page, lp := listParams(page, search)
	if status != "" && !status.Valid() {
		status = ""
	}
	items, total, err := s.orders.List(ctx, port.OrderFilter{ListParams: lp, Status: status})
	if err != nil {
		return port.Page[domain.Order]{}, err
	}
	return newPage(items, total, page), nil
}

func (s *OrderService) ChangeStatus(ctx context.Context, id int, next domain.OrderStatus) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		order, err := s.orders.FindByID(ctx, id)
		if err != nil {
			return err
		}
		if !order.Status.CanTransitionTo(next) {
			return domain.ErrInvalidTransition
		}
		if err := s.orders.UpdateStatus(ctx, id, order.Status, next); err != nil {
			return err
		}
		if next == domain.OrderCanceled {
			// Stock was reserved at creation; give it back.
			for _, it := range order.Items {
				if err := s.products.IncrementStock(ctx, it.ProductID, it.Quantity); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
