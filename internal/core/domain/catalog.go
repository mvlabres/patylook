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
	BrandID      int // 0 for products registered before brands existed
	BrandName    string
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

// Validate checks the fields the user types. The SKU is never typed: it is generated when the
// product is created and kept afterwards, so only its length is checked here.
func (p *Product) Validate() error {
	v := NewValidationError()
	requireText(v, "name", "Nome", p.Name, 150)
	optionalText(v, "description", "Descrição", p.Description, 500)
	optionalText(v, "sku", "SKU", p.SKU, 45)
	if p.CategoryID <= 0 {
		v.Add("product_category_id", "Categoria é obrigatória")
	}
	if p.BrandID < 0 {
		v.Add("brand_id", "Marca inválida")
	}
	if p.Stock < 0 {
		v.Add("stock", "Estoque não pode ser negativo")
	}
	return v.OrNil()
}

// skuTimeLayout is ddMMyyHHmm (24/11/2026 16:02 -> 2411261602).
const skuTimeLayout = "0201061504"

// GenerateSKU builds the SKU of a new product: the first three letters of the category and of
// the brand plus the creation date and time, e.g. PER-FER-2411261602.
func GenerateSKU(category, brand string, at time.Time) string {
	return skuPrefix(category) + "-" + skuPrefix(brand) + "-" + at.Format(skuTimeLayout)
}

var skuAccents = strings.NewReplacer(
	"À", "A", "Á", "A", "Â", "A", "Ã", "A", "Ä", "A", "Å", "A",
	"Ç", "C", "È", "E", "É", "E", "Ê", "E", "Ë", "E",
	"Ì", "I", "Í", "I", "Î", "I", "Ï", "I", "Ñ", "N",
	"Ò", "O", "Ó", "O", "Ô", "O", "Õ", "O", "Ö", "O",
	"Ù", "U", "Ú", "U", "Û", "U", "Ü", "U", "Ý", "Y",
)

// skuPrefix returns up to three upper-case letters or digits of s, ignoring accents, spaces
// and punctuation. A name without any yields "XXX" so the SKU keeps its shape.
func skuPrefix(s string) string {
	var b strings.Builder
	for _, r := range skuAccents.Replace(strings.ToUpper(s)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() == 3 {
				break
			}
		}
	}
	if b.Len() == 0 {
		return "XXX"
	}
	return b.String()
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

// maxMarkup keeps the markup inside MySQL DECIMAL(6,2): 9999.99%.
const maxMarkup Percent = 9999_99

// ComputeMarkup derives MarkupPercent from the informed FinalPrice: the reverse of
// "final price = total cost + markup", i.e. (final - cost) / cost. Without any cost there is
// nothing to apply a percentage to, so the markup is 0.
func (p *Pricebook) ComputeMarkup() {
	cost := p.TotalCost()
	if cost <= 0 {
		p.MarkupPercent = 0
		return
	}
	p.MarkupPercent = Percent(roundDiv(int64(p.FinalPrice-cost)*10000, int64(cost)))
}

// Validate checks the informed values and, when they are usable, fills MarkupPercent.
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
	if p.ValidFrom.IsZero() {
		v.Add("valid_from", "Início da vigência é obrigatório")
	}
	if p.ValidTo != nil && !p.ValidFrom.IsZero() && p.ValidTo.Before(p.ValidFrom) {
		v.Add("valid_to", "Fim da vigência deve ser igual ou posterior ao início")
	}

	costsOK := v.Fields["product_cost"] == "" && v.Fields["packaging_cost"] == "" &&
		v.Fields["logistics_cost"] == "" && v.Fields["other_costs"] == ""
	switch {
	case p.FinalPrice < 0:
		v.Add("final_price", "Preço final não pode ser negativo")
	case p.FinalPrice > maxMoney:
		v.Add("final_price", "Preço final excede o limite permitido")
	case costsOK && p.TotalCost() > maxMoney:
		v.Add("product_cost", "Valor total dos custos excede o limite permitido")
	case costsOK && p.FinalPrice < p.TotalCost():
		v.Add("final_price", "Preço final não pode ser menor que o custo total ("+p.TotalCost().String()+")")
	case costsOK:
		p.ComputeMarkup()
		if p.MarkupPercent > maxMarkup {
			v.Add("final_price", "Preço final muito acima do custo: o markup calculado passa de 9999,99%")
		}
	}
	return v.OrNil()
}
