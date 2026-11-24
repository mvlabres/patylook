package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// ---- in-memory repositories for order tests ----

type memProducts struct {
	port.ProductRepository
	items map[int]*domain.Product
}

func (m *memProducts) FindByID(_ context.Context, id int) (*domain.Product, error) {
	if p, ok := m.items[id]; ok {
		c := *p
		return &c, nil
	}
	return nil, domain.ErrNotFound
}

func (m *memProducts) DecrementStock(_ context.Context, id, qty int) error {
	p := m.items[id]
	if p.Stock < qty {
		return domain.ErrInsufficientStock
	}
	p.Stock -= qty
	return nil
}

func (m *memProducts) IncrementStock(_ context.Context, id, qty int) error {
	m.items[id].Stock += qty
	return nil
}

type memPrices struct {
	port.PricebookRepository
	byProduct map[int]*domain.Pricebook
}

func (m *memPrices) FindCurrent(_ context.Context, id int, _ time.Time) (*domain.Pricebook, error) {
	if p, ok := m.byProduct[id]; ok {
		return p, nil
	}
	return nil, domain.ErrNotFound
}

type memCustomers struct {
	port.CustomerRepository
	items map[int]*domain.Customer
}

func (m *memCustomers) FindByID(_ context.Context, id int) (*domain.Customer, error) {
	if c, ok := m.items[id]; ok {
		return c, nil
	}
	return nil, domain.ErrNotFound
}

type memPayments struct {
	port.PaymentTypeRepository
	items map[int]*domain.PaymentType
}

func (m *memPayments) FindByID(_ context.Context, id int) (*domain.PaymentType, error) {
	if p, ok := m.items[id]; ok {
		return p, nil
	}
	return nil, domain.ErrNotFound
}

// memOrders stores orders; rollback of failed transactions is emulated by memTx.
type memOrders struct {
	port.OrderRepository
	items  map[int]*domain.Order
	nextID int
}

func (m *memOrders) Create(_ context.Context, o *domain.Order) error {
	m.nextID++
	o.ID = m.nextID
	c := *o
	m.items[o.ID] = &c
	return nil
}

func (m *memOrders) FindByID(_ context.Context, id int) (*domain.Order, error) {
	if o, ok := m.items[id]; ok {
		c := *o
		return &c, nil
	}
	return nil, domain.ErrNotFound
}

func (m *memOrders) UpdateStatus(_ context.Context, id int, from, to domain.OrderStatus) error {
	o := m.items[id]
	if o.Status != from {
		return domain.ErrConflict
	}
	o.Status = to
	return nil
}

// memTx snapshots stock before running fn and restores it when fn fails.
type memTx struct {
	products *memProducts
	orders   *memOrders
}

func (m *memTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	stock := map[int]int{}
	for id, p := range m.products.items {
		stock[id] = p.Stock
	}
	ids := len(m.orders.items)
	nextID := m.orders.nextID
	if err := fn(ctx); err != nil {
		for id, s := range stock {
			m.products.items[id].Stock = s
		}
		if len(m.orders.items) != ids {
			m.orders.items = map[int]*domain.Order{}
			m.orders.nextID = nextID
		}
		return err
	}
	return nil
}

type orderEnv struct {
	svc      *OrderService
	products *memProducts
	orders   *memOrders
}

func newOrderEnv() *orderEnv {
	products := &memProducts{items: map[int]*domain.Product{
		1: {ID: 1, Name: "Camiseta", SKU: "CAM-1", IsActive: true, Stock: 10},
		2: {ID: 2, Name: "Caneca", SKU: "CAN-1", IsActive: true, Stock: 3},
		3: {ID: 3, Name: "Sem preço", SKU: "SP-1", IsActive: true, Stock: 5},
		4: {ID: 4, Name: "Inativo", SKU: "IN-1", IsActive: false, Stock: 5},
	}}
	prices := &memPrices{byProduct: map[int]*domain.Pricebook{
		1: {ID: 11, ProductID: 1, ProductCost: 3000, PackagingCost: 500, MarkupPercent: 5000, FinalPrice: 5250},
		2: {ID: 12, ProductID: 2, ProductCost: 1000, FinalPrice: 2000},
		4: {ID: 14, ProductID: 4, ProductCost: 1000, FinalPrice: 2000},
	}}
	customers := &memCustomers{items: map[int]*domain.Customer{
		1: {ID: 1, Name: "Cliente", IsActive: true},
		2: {ID: 2, Name: "Cliente inativo", IsActive: false},
	}}
	payments := &memPayments{items: map[int]*domain.PaymentType{
		1: {ID: 1, Name: "Cartão", SurchargePercent: 500},
		2: {ID: 2, Name: "PIX"},
	}}
	orders := &memOrders{items: map[int]*domain.Order{}}
	clock := newClock()
	return &orderEnv{
		svc:      NewOrderService(orders, products, prices, customers, payments, &memTx{products, orders}, clock.now),
		products: products,
		orders:   orders,
	}
}

var seller = &domain.User{ID: 9}

func TestCreateOrderSnapshotsPricesAndReservesStock(t *testing.T) {
	env := newOrderEnv()
	ctx := context.Background()

	order, err := env.svc.Create(ctx, seller, port.CreateOrderInput{
		CustomerID: 1, PaymentTypeID: 1, Discount: 500, Notes: " obs ",
		Items: []port.OrderItemInput{
			{ProductID: 2, Quantity: 1},
			{ProductID: 1, Quantity: 2, Discount: 250},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 2 x 52,50 - 2,50 = 102,50 ; + 1 x 20,00 = 122,50 ; - 5,00 = 117,50 ; +5% = 5,88 (round half up)
	if order.Subtotal != 12250 || order.PaymentAddition != 588 || order.Total != 12338 {
		t.Errorf("subtotal=%d addition=%d total=%d", order.Subtotal, order.PaymentAddition, order.Total)
	}
	if order.Status != domain.OrderOpen || order.UserID != 9 || order.Notes != "obs" {
		t.Errorf("unexpected order: %+v", order)
	}
	if order.Items[0].ProductID != 1 {
		t.Error("items should be ordered by product id")
	}
	if got := order.Items[0]; got.UnitPrice != 5250 || got.UnitCost != 3500 || got.PricebookID == nil || *got.PricebookID != 11 {
		t.Errorf("price/cost snapshot wrong: %+v", got)
	}
	if env.products.items[1].Stock != 8 || env.products.items[2].Stock != 2 {
		t.Errorf("stock = %d, %d", env.products.items[1].Stock, env.products.items[2].Stock)
	}
}

func TestCreateOrderRejections(t *testing.T) {
	cases := map[string]port.CreateOrderInput{
		"inactive customer": {CustomerID: 2, PaymentTypeID: 2, Items: []port.OrderItemInput{{ProductID: 1, Quantity: 1}}},
		"unknown customer":  {CustomerID: 99, PaymentTypeID: 2, Items: []port.OrderItemInput{{ProductID: 1, Quantity: 1}}},
		"unknown payment":   {CustomerID: 1, PaymentTypeID: 99, Items: []port.OrderItemInput{{ProductID: 1, Quantity: 1}}},
		"no items":          {CustomerID: 1, PaymentTypeID: 2},
		"no price":          {CustomerID: 1, PaymentTypeID: 2, Items: []port.OrderItemInput{{ProductID: 3, Quantity: 1}}},
		"inactive product":  {CustomerID: 1, PaymentTypeID: 2, Items: []port.OrderItemInput{{ProductID: 4, Quantity: 1}}},
		"unknown product":   {CustomerID: 1, PaymentTypeID: 2, Items: []port.OrderItemInput{{ProductID: 99, Quantity: 1}}},
		"zero quantity":     {CustomerID: 1, PaymentTypeID: 2, Items: []port.OrderItemInput{{ProductID: 1, Quantity: 0}}},
		"duplicate product": {CustomerID: 1, PaymentTypeID: 2, Items: []port.OrderItemInput{{ProductID: 1, Quantity: 1}, {ProductID: 1, Quantity: 1}}},
		"discount too big":  {CustomerID: 1, PaymentTypeID: 2, Discount: 999999, Items: []port.OrderItemInput{{ProductID: 1, Quantity: 1}}},
	}
	for name, in := range cases {
		env := newOrderEnv()
		_, err := env.svc.Create(context.Background(), seller, in)
		var ve *domain.ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: expected ValidationError, got %v", name, err)
		}
		if len(env.orders.items) != 0 || env.products.items[1].Stock != 10 {
			t.Errorf("%s: nothing should be persisted", name)
		}
	}
}

func TestCreateOrderInsufficientStockRollsBack(t *testing.T) {
	env := newOrderEnv()
	_, err := env.svc.Create(context.Background(), seller, port.CreateOrderInput{
		CustomerID: 1, PaymentTypeID: 2,
		Items: []port.OrderItemInput{
			{ProductID: 1, Quantity: 5},
			{ProductID: 2, Quantity: 4}, // only 3 in stock
		},
	})
	fields, ok := func() (map[string]string, bool) {
		var ve *domain.ValidationError
		if errors.As(err, &ve) {
			return ve.Fields, true
		}
		return nil, false
	}()
	if !ok || fields["items"] == "" {
		t.Fatalf("expected stock validation error, got %v", err)
	}
	if env.products.items[1].Stock != 10 || env.products.items[2].Stock != 3 {
		t.Errorf("stock must be restored, got %d and %d", env.products.items[1].Stock, env.products.items[2].Stock)
	}
	if len(env.orders.items) != 0 {
		t.Error("order must not be persisted")
	}
}

func TestChangeStatus(t *testing.T) {
	env := newOrderEnv()
	ctx := context.Background()
	order, err := env.svc.Create(ctx, seller, port.CreateOrderInput{
		CustomerID: 1, PaymentTypeID: 2, Items: []port.OrderItemInput{{ProductID: 1, Quantity: 4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if env.products.items[1].Stock != 6 {
		t.Fatalf("stock after order = %d", env.products.items[1].Stock)
	}

	if err := env.svc.ChangeStatus(ctx, order.ID, domain.OrderCompleted); err != nil {
		t.Fatal(err)
	}
	if env.products.items[1].Stock != 6 {
		t.Error("completing must not touch stock")
	}
	if err := env.svc.ChangeStatus(ctx, order.ID, domain.OrderOpen); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("reopen: %v", err)
	}
	if err := env.svc.ChangeStatus(ctx, order.ID, domain.OrderCanceled); err != nil {
		t.Fatal(err)
	}
	if env.products.items[1].Stock != 10 {
		t.Errorf("cancel must return stock, got %d", env.products.items[1].Stock)
	}
	if err := env.svc.ChangeStatus(ctx, order.ID, domain.OrderCanceled); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("double cancel: %v", err)
	}
	if env.products.items[1].Stock != 10 {
		t.Error("double cancel must not return stock twice")
	}
	if err := env.svc.ChangeStatus(ctx, 999, domain.OrderCanceled); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown order: %v", err)
	}
}
