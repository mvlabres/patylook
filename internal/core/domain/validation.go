package domain

import (
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"
)

func requireText(v *ValidationError, field, label, value string, max int) {
	switch {
	case strings.TrimSpace(value) == "":
		v.Add(field, label+" é obrigatório")
	case utf8.RuneCountInString(value) > max:
		v.Add(field, label+" deve ter no máximo "+strconv.Itoa(max)+" caracteres")
	}
}

func optionalText(v *ValidationError, field, label, value string, max int) {
	if utf8.RuneCountInString(value) > max {
		v.Add(field, label+" deve ter no máximo "+strconv.Itoa(max)+" caracteres")
	}
}

func validEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}
