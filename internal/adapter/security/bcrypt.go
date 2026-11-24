// Package security provides cryptographic adapters.
package security

import (
	"golang.org/x/crypto/bcrypt"

	"patylook/internal/core/port"
)

const bcryptCost = 12

type BcryptHasher struct{ cost int }

var _ port.PasswordHasher = BcryptHasher{}

func NewBcryptHasher() BcryptHasher { return BcryptHasher{cost: bcryptCost} }

func (h BcryptHasher) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Verify returns false for wrong passwords and for hashes that are not valid bcrypt
// (for instance legacy values stored in the original VARCHAR(32) column).
func (h BcryptHasher) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
