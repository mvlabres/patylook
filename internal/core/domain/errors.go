package domain

import (
	"errors"
	"sort"
	"strings"
)

// Sentinel errors returned by services and repositories. Adapters translate them
// into transport-specific responses.
var (
	ErrNotFound           = errors.New("registro não encontrado")
	ErrConflict           = errors.New("registro duplicado ou em conflito")
	ErrInUse              = errors.New("registro em uso por outros dados")
	ErrInvalidCredentials = errors.New("usuário ou senha inválidos")
	ErrUnauthenticated    = errors.New("autenticação necessária")
	ErrForbidden          = errors.New("acesso negado")
	ErrTooManyAttempts    = errors.New("muitas tentativas, tente novamente mais tarde")
	ErrInsufficientStock  = errors.New("estoque insuficiente")
	ErrInvalidTransition  = errors.New("transição de status inválida")
)

// ConflictError is a unique-constraint violation on a specific field. It matches ErrConflict.
type ConflictError struct {
	Field string
}

func (e *ConflictError) Error() string        { return ErrConflict.Error() + ": " + e.Field }
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// ValidationError groups field-level validation messages. The empty key holds
// messages that do not belong to a specific field.
type ValidationError struct {
	Fields map[string]string
}

func NewValidationError() *ValidationError {
	return &ValidationError{Fields: map[string]string{}}
}

// Add records a message for a field, keeping the first one reported.
func (v *ValidationError) Add(field, msg string) {
	if _, ok := v.Fields[field]; !ok {
		v.Fields[field] = msg
	}
}

// OrNil returns the error only if at least one message was recorded.
func (v *ValidationError) OrNil() error {
	if len(v.Fields) == 0 {
		return nil
	}
	return v
}

func (v *ValidationError) Error() string {
	keys := make([]string, 0, len(v.Fields))
	for k := range v.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	msgs := make([]string, 0, len(keys))
	for _, k := range keys {
		msgs = append(msgs, v.Fields[k])
	}
	return strings.Join(msgs, "; ")
}
