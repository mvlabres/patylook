package domain

import (
	"strings"
	"time"
)

// Brand is the manufacturer/label a product can be linked to (product.brand_id).
type Brand struct {
	ID            int
	Name          string
	IsActive      bool
	CreatedBy     int
	CreatedByName string // read-only projection
	CreatedAt     time.Time
}

func (b *Brand) Normalize() {
	b.Name = strings.TrimSpace(b.Name)
}

func (b *Brand) Validate() error {
	v := NewValidationError()
	requireText(v, "name", "Nome", b.Name, 100)
	return v.OrNil()
}
