package domain

import (
	"strings"
	"time"
)

type OrderStatus string

const (
	OrderOpen      OrderStatus = "open"
	OrderCompleted OrderStatus = "completed"
	OrderCanceled  OrderStatus = "canceled"
)

func (s OrderStatus) Valid() bool {
	return s == OrderOpen || s == OrderCompleted || s == OrderCanceled
}

// Label is the Portuguese display name of the status.
func (s OrderStatus) Label() string {
	switch s {
	case OrderOpen:
		return "Aberto"
	case OrderCompleted:
		return "Concluído"
	case OrderCanceled:
		return "Cancelado"
	}
	return string(s)
}

// CanTransitionTo reports whether the order may move from s to next.
func (s OrderStatus) CanTransitionTo(next OrderStatus) bool {
	switch s {
	case OrderOpen:
		return next == OrderCompleted || next == OrderCanceled
	case OrderCompleted:
		return next == OrderCanceled
	}
	return false
}

const maxOrderNotes = 2000

type OrderItem struct {
	ProductID   int
	Quantity    int
	UnitPrice   Money
	UnitCost    Money
	Discount    Money // total discount applied to the line
	PricebookID *int

	ProductName string // read-only projections
	ProductSKU  string
}

func (i OrderItem) Gross() Money { return i.UnitPrice.Mul(i.Quantity) }
func (i OrderItem) Total() Money { return i.Gross() - i.Discount }

type Order struct {
	ID              int
	UserID          int
	CustomerID      int
	PaymentTypeID   int
	Status          OrderStatus
	Subtotal        Money
	Discount        Money
	PaymentAddition Money
	Total           Money
	Notes           string
	CreatedAt       time.Time
	Items           []OrderItem

	UserName        string // read-only projections
	CustomerName    string
	PaymentTypeName string
}

func (o *Order) Normalize() {
	o.Notes = strings.TrimSpace(o.Notes)
}

// Calculate validates the order and derives Subtotal, PaymentAddition and Total.
// The payment surcharge applies over the subtotal after the order discount.
func (o *Order) Calculate(surcharge Percent) error {
	v := NewValidationError()
	if o.CustomerID <= 0 {
		v.Add("customer_id", "Cliente é obrigatório")
	}
	if o.PaymentTypeID <= 0 {
		v.Add("payment_type_id", "Forma de pagamento é obrigatória")
	}
	optionalText(v, "notes", "Observações", o.Notes, maxOrderNotes)
	if len(o.Items) == 0 {
		v.Add("items", "Adicione ao menos um produto")
	}

	seen := make(map[int]bool, len(o.Items))
	var subtotal Money
	for _, it := range o.Items {
		switch {
		case it.ProductID <= 0:
			v.Add("items", "Produto inválido")
		case seen[it.ProductID]:
			v.Add("items", "Produto repetido no pedido")
		case it.Quantity <= 0:
			v.Add("items", "Quantidade deve ser maior que zero")
		case it.Discount < 0 || it.Discount > it.Gross():
			v.Add("items", "Desconto do item deve estar entre zero e o valor do item")
		}
		seen[it.ProductID] = true
		subtotal += it.Total()
	}
	if o.Discount < 0 || o.Discount > subtotal {
		v.Add("discount", "Desconto deve estar entre zero e o subtotal")
	}
	if err := v.OrNil(); err != nil {
		return err
	}

	o.Subtotal = subtotal
	o.PaymentAddition = (subtotal - o.Discount).Percent(surcharge)
	o.Total = subtotal - o.Discount + o.PaymentAddition
	return nil
}
