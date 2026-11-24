package mysql

import (
	"database/sql"
	"errors"
	"testing"

	drv "github.com/go-sql-driver/mysql"

	"patylook/internal/core/domain"
)

func TestTranslate(t *testing.T) {
	var ce *domain.ConflictError
	err := translate(&drv.MySQLError{Number: errDuplicateEntry, Message: "Duplicate entry 'X' for key 'product.sku_UNIQUE'"})
	if !errors.As(err, &ce) || ce.Field != "sku" || !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate: %v", err)
	}
	if err := translate(&drv.MySQLError{Number: errRowIsReferenced}); !errors.Is(err, domain.ErrInUse) {
		t.Errorf("referenced row: %v", err)
	}
	if err := translate(sql.ErrNoRows); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("no rows: %v", err)
	}
	if translate(nil) != nil {
		t.Error("nil must stay nil")
	}
	boom := errors.New("boom")
	if translate(boom) != boom {
		t.Error("unknown errors must pass through")
	}
}

func TestDuplicateField(t *testing.T) {
	tests := map[string]string{
		"Duplicate entry 'a' for key 'user.username_UNIQUE'":     "username", // MySQL 8
		"Duplicate entry 'a' for key 'email_UNIQUE'":             "email",    // MySQL 5.7 / MariaDB
		"Duplicate entry 'a' for key 'customer.document_UNIQUE'": "document",
		"Duplicate entry '1-2' for key 'order_item.PRIMARY'":     "",
		"something unexpected":                                   "",
	}
	for msg, want := range tests {
		if got := duplicateField(msg); got != want {
			t.Errorf("duplicateField(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestLikePatternEscapesWildcards(t *testing.T) {
	if got := likePattern(`50%_off\`); got != `%50\%\_off\\%` {
		t.Errorf("likePattern = %q", got)
	}
}

func TestDecimalColumns(t *testing.T) {
	var m domain.Money
	if err := (moneyCol{&m}).Scan([]byte("1234.50")); err != nil || m != 123450 {
		t.Errorf("money = %d, %v", m, err)
	}
	var p domain.Percent
	if err := (percentCol{&p}).Scan("12.50"); err != nil || p != 1250 {
		t.Errorf("percent = %d, %v", p, err)
	}
	var np *domain.Money
	if err := (nullMoneyCol{&np}).Scan(nil); err != nil || np != nil {
		t.Errorf("null money = %v, %v", np, err)
	}
	if err := (nullMoneyCol{&np}).Scan([]byte("0.99")); err != nil || np == nil || *np != 99 {
		t.Errorf("non-null money = %v, %v", np, err)
	}
	if err := (moneyCol{&m}).Scan(true); err == nil {
		t.Error("unsupported type accepted")
	}
}

func TestFormatOffset(t *testing.T) {
	for secs, want := range map[int]string{-10800: "-03:00", 0: "+00:00", 19800: "+05:30"} {
		if got := formatOffset(secs); got != want {
			t.Errorf("formatOffset(%d) = %q, want %q", secs, got, want)
		}
	}
}
