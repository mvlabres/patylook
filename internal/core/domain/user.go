package domain

import (
	"strconv"
	"strings"
	"time"
)

const MinPasswordLength = 8

// MaxPasswordLength is bcrypt's input limit in bytes.
const MaxPasswordLength = 72

type UserCategory struct {
	ID   int
	Name string
}

type User struct {
	ID           int
	Username     string
	Email        string // empty means NULL
	Name         string
	PasswordHash string
	CategoryID   int
	CategoryName string
	IsActive     bool
	CreatedAt    time.Time

	// Admin is derived by the services from the configured admin category; it is not persisted.
	Admin bool
}

func (u *User) Normalize() {
	u.Username = strings.TrimSpace(u.Username)
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	u.Name = strings.TrimSpace(u.Name)
}

func (u *User) Validate() error {
	v := NewValidationError()
	requireText(v, "username", "Usuário", u.Username, 16)
	if u.Username != "" && len(u.Username) < 3 {
		v.Add("username", "Usuário deve ter ao menos 3 caracteres")
	}
	for _, r := range u.Username {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-') {
			v.Add("username", "Usuário aceita apenas letras, números, ponto, hífen e sublinhado")
			break
		}
	}
	requireText(v, "name", "Nome", u.Name, 100)
	if u.Email != "" {
		optionalText(v, "email", "E-mail", u.Email, 255)
		if !validEmail(u.Email) {
			v.Add("email", "E-mail inválido")
		}
	}
	if u.CategoryID <= 0 {
		v.Add("user_category_id", "Categoria é obrigatória")
	}
	return v.OrNil()
}

// ValidatePassword checks the password policy.
func ValidatePassword(field, password string) error {
	v := NewValidationError()
	switch {
	case len(password) < MinPasswordLength:
		v.Add(field, "Senha deve ter ao menos "+strconv.Itoa(MinPasswordLength)+" caracteres")
	case len(password) > MaxPasswordLength:
		v.Add(field, "Senha deve ter no máximo "+strconv.Itoa(MaxPasswordLength)+" caracteres")
	}
	return v.OrNil()
}
