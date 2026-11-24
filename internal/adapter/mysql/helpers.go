package mysql

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"patylook/internal/core/domain"
)

const (
	errDuplicateEntry   = 1062
	errRowIsReferenced  = 1451
	errNoReferencedRow  = 1452
	errNoReferencedRow2 = 1216
)

// translate maps driver errors to domain errors.
func translate(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case errDuplicateEntry:
			return &domain.ConflictError{Field: duplicateField(me.Message)}
		case errRowIsReferenced:
			return domain.ErrInUse
		case errNoReferencedRow, errNoReferencedRow2:
			return domain.ErrNotFound
		}
	}
	return err
}

// duplicateField extracts the column name from "Duplicate entry 'x' for key 'table.name_UNIQUE'".
func duplicateField(msg string) string {
	i := strings.LastIndex(msg, "for key '")
	if i < 0 {
		return ""
	}
	key := msg[i+len("for key '"):]
	if j := strings.IndexByte(key, '\''); j >= 0 {
		key = key[:j]
	}
	if j := strings.LastIndexByte(key, '.'); j >= 0 {
		key = key[j+1:]
	}
	if strings.EqualFold(key, "PRIMARY") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimSuffix(key, "_UNIQUE"), "_unique")
}

// ---- value helpers ----

func nullString(s string) sql.NullString { return sql.NullString{String: s, Valid: s != ""} }

func dateParam(t time.Time) string { return t.Format("2006-01-02") }

func nullDate(t *time.Time) any {
	if t == nil {
		return nil
	}
	return dateParam(*t)
}

// likePattern builds a contains pattern with LIKE wildcards escaped.
func likePattern(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// moneyCol scans a DECIMAL column into domain.Money without going through floats.
type moneyCol struct{ dst *domain.Money }

func (c moneyCol) Scan(src any) error {
	s, err := decimalString(src)
	if err != nil {
		return err
	}
	v, err := domain.ParseMoney(s)
	if err != nil {
		return fmt.Errorf("decimal %q: %w", s, err)
	}
	*c.dst = v
	return nil
}

// percentCol scans a DECIMAL column into domain.Percent.
type percentCol struct{ dst *domain.Percent }

func (c percentCol) Scan(src any) error {
	s, err := decimalString(src)
	if err != nil {
		return err
	}
	v, err := domain.ParsePercent(s)
	if err != nil {
		return fmt.Errorf("decimal %q: %w", s, err)
	}
	*c.dst = v
	return nil
}

// nullMoneyCol scans a nullable DECIMAL into *domain.Money (nil when NULL).
type nullMoneyCol struct{ dst **domain.Money }

func (c nullMoneyCol) Scan(src any) error {
	if src == nil {
		*c.dst = nil
		return nil
	}
	var m domain.Money
	if err := (moneyCol{&m}).Scan(src); err != nil {
		return err
	}
	*c.dst = &m
	return nil
}

func decimalString(src any) (string, error) {
	switch v := src.(type) {
	case []byte:
		return string(v), nil
	case string:
		return v, nil
	case int64:
		return fmt.Sprintf("%d", v), nil
	case float64:
		return fmt.Sprintf("%.2f", v), nil
	}
	return "", fmt.Errorf("tipo %T não suportado para DECIMAL", src)
}

func timeOrZero(t sql.NullTime) time.Time {
	if t.Valid {
		return t.Time
	}
	return time.Time{}
}

// scanner is satisfied by *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }
