package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Money represents a monetary amount in cents, avoiding floating point errors.
type Money int64

// Percent represents a percentage in hundredths (12.50% == 1250).
type Percent int64

var errInvalidNumber = errors.New("número inválido")

// ParseMoney accepts "1234.56", "1234,56" and "1.234,56".
func ParseMoney(s string) (Money, error) {
	v, err := parseFixed2(s)
	return Money(v), err
}

// ParsePercent accepts the same formats as ParseMoney.
func ParsePercent(s string) (Percent, error) {
	v, err := parseFixed2(s)
	return Percent(v), err
}

func parseFixed2(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errInvalidNumber
	}
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" && fracPart == "" {
		return 0, errInvalidNumber
	}
	if len(fracPart) > 2 || !onlyDigits(intPart) || !onlyDigits(fracPart) {
		return 0, errInvalidNumber
	}
	fracPart += strings.Repeat("0", 2-len(fracPart))
	if intPart == "" {
		intPart = "0"
	}
	i, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return 0, errInvalidNumber
	}
	f, _ := strconv.ParseInt(fracPart, 10, 64)
	v := i*100 + f
	if neg {
		v = -v
	}
	return v, nil
}

func onlyDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Decimal returns the value as a plain decimal string ("12.50"), the format used by MySQL DECIMAL.
func (m Money) Decimal() string { return fixed2(int64(m), ".") }

// String formats the value in Brazilian Real ("R$ 1.234,56").
func (m Money) String() string {
	sign := ""
	v := int64(m)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%sR$ %s", sign, groupThousands(v/100)+","+fmt.Sprintf("%02d", v%100))
}

// Input returns the value formatted for HTML inputs ("1234,56").
func (m Money) Input() string { return fixed2(int64(m), ",") }

// Mul multiplies by an integer quantity.
func (m Money) Mul(qty int) Money { return m * Money(qty) }

// Percent returns p% of m, rounded half away from zero.
func (m Money) Percent(p Percent) Money {
	return Money(roundDiv(int64(m)*int64(p), 10000))
}

// Decimal returns the percentage as a plain decimal string ("12.50").
func (p Percent) Decimal() string { return fixed2(int64(p), ".") }

// Input returns the percentage formatted for HTML inputs ("12,50").
func (p Percent) Input() string { return fixed2(int64(p), ",") }

func (p Percent) String() string { return p.Input() + "%" }

func fixed2(v int64, sep string) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d%s%02d", sign, v/100, sep, v%100)
}

func groupThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

func roundDiv(n, d int64) int64 {
	if n < 0 {
		return -roundDiv(-n, d)
	}
	return (n + d/2) / d
}
