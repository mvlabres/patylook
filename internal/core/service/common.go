package service

import (
	"errors"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

// PageSize is the number of rows shown per list page.
const PageSize = 20

// Clock returns the current time in the application's location.
type Clock func() time.Time

// dateOf truncates t to midnight in its own location.
func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func listParams(page int, search string) (int, port.ListParams) {
	if page < 1 {
		page = 1
	}
	return page, port.ListParams{Search: search, Limit: PageSize, Offset: (page - 1) * PageSize}
}

func newPage[T any](items []T, total, page int) port.Page[T] {
	return port.Page[T]{Items: items, Total: total, Page: page, PageSize: PageSize}
}

// conflictToValidation turns a repository ConflictError into a field validation
// error using the given messages (keyed by field name).
func conflictToValidation(err error, messages map[string]string) error {
	var ce *domain.ConflictError
	if !errors.As(err, &ce) {
		return err
	}
	v := domain.NewValidationError()
	msg, ok := messages[ce.Field]
	if !ok {
		msg = "Já existe um registro com esse valor"
	}
	v.Add(ce.Field, msg)
	return v
}

// adminRule decides which user category is administrative.
type adminRule string

func (r adminRule) mark(u *domain.User) *domain.User {
	if u != nil {
		u.Admin = u.CategoryName == string(r)
	}
	return u
}
