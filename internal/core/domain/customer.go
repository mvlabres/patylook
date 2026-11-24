package domain

import (
	"strings"
	"time"
)

type Customer struct {
	ID        int
	Name      string
	Document  string // CPF, digits only
	Phone     string
	Email     string
	IsActive  bool
	CreatedAt time.Time
}

func (c *Customer) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.Document = OnlyDigits(c.Document)
	c.Phone = strings.TrimSpace(c.Phone)
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
}

func (c *Customer) Validate() error {
	v := NewValidationError()
	requireText(v, "name", "Nome", c.Name, 100)
	if !ValidCPF(c.Document) {
		v.Add("document", "CPF inválido")
	}
	requireText(v, "phone", "Telefone", c.Phone, 20)
	requireText(v, "email", "E-mail", c.Email, 45)
	if c.Email != "" && !validEmail(c.Email) {
		v.Add("email", "E-mail inválido")
	}
	return v.OrNil()
}

// OnlyDigits strips every non-digit character.
func OnlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidCPF validates the length and check digits of a CPF (digits only).
func ValidCPF(cpf string) bool {
	if len(cpf) != 11 {
		return false
	}
	same := true
	for i := 1; i < 11; i++ {
		if cpf[i] != cpf[0] {
			same = false
			break
		}
	}
	if same {
		return false
	}
	return cpfDigit(cpf[:9]) == int(cpf[9]-'0') && cpfDigit(cpf[:10]) == int(cpf[10]-'0')
}

func cpfDigit(s string) int {
	sum := 0
	for i, w := 0, len(s)+1; i < len(s); i, w = i+1, w-1 {
		sum += int(s[i]-'0') * w
	}
	d := (sum * 10) % 11
	if d == 10 {
		d = 0
	}
	return d
}
