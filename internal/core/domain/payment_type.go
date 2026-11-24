package domain

import "strings"

// PaymentType only classifies how the customer intends to pay (and any surcharge);
// there is no payment processing in this system.
type PaymentType struct {
	ID               int
	Name             string
	Description      string
	SurchargePercent Percent
}

func (p *PaymentType) Normalize() {
	p.Name = strings.TrimSpace(p.Name)
	p.Description = strings.TrimSpace(p.Description)
}

func (p *PaymentType) Validate() error {
	v := NewValidationError()
	requireText(v, "name", "Nome", p.Name, 45)
	optionalText(v, "description", "Descrição", p.Description, 45)
	if p.SurchargePercent < 0 || p.SurchargePercent > 999_99 {
		v.Add("surcharge_percent", "Acréscimo deve estar entre 0 e 999,99")
	}
	return v.OrNil()
}
