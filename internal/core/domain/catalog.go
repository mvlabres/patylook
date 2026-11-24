package domain

import (
	"strings"
	"time"
)

type ProductCategory struct {
	ID          int
	Name        string
	Description string
}

func (c *ProductCategory) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Description = strings.TrimSpace(c.Description)
}

func (c *ProductCategory) Validate() error {
	v := NewValidationError()
	requireText(v, "name", "Nome", c.Name, 45)
	optionalText(v, "description", "Descrição", c.Description, 45)
	return v.OrNil()
}

type Product struct {
	ID           int
	Name         string
	Description  string
	SKU          string
	CategoryID   int
	CategoryName string
	Stock        int
	IsActive     bool
	CreatedBy    int
	CreatedAt    time.Time

	// CurrentPrice is the price valid today, when one exists. Read-only projection.
	CurrentPrice *Money
}

func (p *Product) Normalize() {
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
	p.SKU = strings.ToUpper(strings.TrimSpace(p.SKU))
}

func (p *Product) Validate() error {
	v := NewValidationError()
	requireText(v, "name", "Nome", p.Name, 150)
	optionalText(v, "description", "Descrição", p.Description, 500)
	requireText(v, "sku", "SKU", p.SKU, 45)
	if p.CategoryID <= 0 {
		v.Add("product_category_id", "Categoria é obrigatória")
	}
	if p.Stock < 0 {
		v.Add("stock", "Estoque não pode ser negativo")
	}
	return v.OrNil()
}

// maxMoney keeps values inside MySQL DECIMAL(12,2).
const maxMoney Money = 9_999_999_999_99

// Pricebook is a price entry for a product, valid for a date range.
type Pricebook struct {
	ID            int
	ProductID     int
	ProductCost   Money
	MarkupPercent Percent
	PackagingCost Money
	LogisticsCost Money
	OtherCosts    Money
	FinalPrice    Money
	ValidFrom     time.Time // date only
	ValidTo       *time.Time
	CreatedBy     int
	CreatedByName string
	CreatedAt     time.Time
}

// TotalCost is everything the business spends to sell one unit.
func (p *Pricebook) TotalCost() Money {
	return p.ProductCost + p.PackagingCost + p.LogisticsCost + p.OtherCosts
}

// ComputeFinalPrice sets FinalPrice as total cost plus markup.
func (p *Pricebook) ComputeFinalPrice() {
	cost := p.TotalCost()
	p.FinalPrice = cost + cost.Percent(p.MarkupPercent)
}

func (p *Pricebook) Validate() error {
	v := NewValidationError()
	if p.ProductCost < 0 {
		v.Add("product_cost", "Custo do produto não pode ser negativo")
	}
	if p.PackagingCost < 0 {
		v.Add("packaging_cost", "Custo de embalagem não pode ser negativo")
	}
	if p.LogisticsCost < 0 {
		v.Add("logistics_cost", "Custo logístico não pode ser negativo")
	}
	if p.OtherCosts < 0 {
		v.Add("other_costs", "Outros custos não podem ser negativos")
	}
	if p.MarkupPercent < 0 || p.MarkupPercent > 9999_99 {
		v.Add("markup_percent", "Markup deve estar entre 0 e 9999,99")
	}
	if p.ValidFrom.IsZero() {
		v.Add("valid_from", "Início da vigência é obrigatório")
	}
	if p.ValidTo != nil && !p.ValidFrom.IsZero() && p.ValidTo.Before(p.ValidFrom) {
		v.Add("valid_to", "Fim da vigência deve ser igual ou posterior ao início")
	}
	if len(v.Fields) == 0 {
		p.ComputeFinalPrice()
		if p.FinalPrice > maxMoney {
			v.Add("product_cost", "Valor total excede o limite permitido")
		}
	}
	return v.OrNil()
}
