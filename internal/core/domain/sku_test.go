package domain

import (
	"testing"
	"time"
)

func TestGenerateSKU(t *testing.T) {
	at := time.Date(2026, 11, 24, 16, 2, 0, 0, time.UTC)
	tests := []struct {
		category, brand, want string
	}{
		{"Perfumaria", "Ferrari", "PER-FER-2411261602"},
		{"perfumaria", "ferrari", "PER-FER-2411261602"},
		{"Água", "Ação & Cia", "AGU-ACA-2411261602"},        // accents folded
		{"Hidratante", "O Boticário", "HID-OBO-2411261602"}, // spaces skipped
		{"Kit", "3M", "KIT-3M-2411261602"},                  // fewer than three characters
		{"!!!", "Natura", "XXX-NAT-2411261602"},             // nothing usable
	}
	for _, tt := range tests {
		if got := GenerateSKU(tt.category, tt.brand, at); got != tt.want {
			t.Errorf("GenerateSKU(%q, %q) = %q, want %q", tt.category, tt.brand, got, tt.want)
		}
	}
}

func TestGenerateSKUUsesDayMonthYearHourMinute(t *testing.T) {
	// 05/01/2027 09:07 must keep the leading zeros: ddMMyyHHmm.
	got := GenerateSKU("Perfumaria", "Ferrari", time.Date(2027, 1, 5, 9, 7, 59, 0, time.UTC))
	if got != "PER-FER-0501270907" {
		t.Errorf("got %q", got)
	}
}

func TestGeneratedSKUIsAValidImageFolder(t *testing.T) {
	sku := GenerateSKU("!!!", "???", time.Date(2026, 11, 24, 16, 2, 0, 0, time.UTC))
	folder, err := ImageFolder(sku)
	if err != nil || folder != sku {
		t.Errorf("ImageFolder(%q) = %q, %v", sku, folder, err)
	}
}
