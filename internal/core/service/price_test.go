package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"patylook/internal/core/domain"
	"patylook/internal/core/port"
)

type recordingPrices struct {
	port.PricebookRepository
	created []domain.Pricebook
}

func (m *recordingPrices) Create(_ context.Context, p *domain.Pricebook) error {
	m.created = append(m.created, *p)
	return nil
}

func newPriceEnv() (*CatalogService, *recordingPrices) {
	products := &catalogProducts{items: map[int]*domain.Product{1: {ID: 1, SKU: "PER-FER-2411261000", Name: "Perfume"}}}
	prices := &recordingPrices{}
	svc := NewCatalogService(nil, nil, products, prices, nil, nil, nil, newClock().now,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, prices
}

func TestAddPriceStoresMarkupDerivedFromFinalPrice(t *testing.T) {
	svc, prices := newPriceEnv()
	entry := &domain.Pricebook{
		ProductID: 1, ProductCost: 5000, PackagingCost: 500, LogisticsCost: 300, OtherCosts: 200, // 60,00
		FinalPrice: 9000, ValidFrom: time.Date(2026, 11, 24, 15, 30, 0, 0, testLoc),
	}
	// A markup sent by a client is not an input: it is always recalculated.
	entry.MarkupPercent = 1

	if err := svc.AddPrice(context.Background(), &domain.User{ID: 7}, entry); err != nil {
		t.Fatal(err)
	}
	if len(prices.created) != 1 {
		t.Fatalf("created = %d", len(prices.created))
	}
	got := prices.created[0]
	if got.MarkupPercent != 5000 || got.FinalPrice != 9000 || got.CreatedBy != 7 {
		t.Errorf("stored: markup=%d final=%d by=%d", got.MarkupPercent, got.FinalPrice, got.CreatedBy)
	}
	if got.ValidFrom.Hour() != 0 {
		t.Errorf("valid_from must be a date, got %v", got.ValidFrom)
	}
}

func TestAddPriceRefusesPriceBelowCost(t *testing.T) {
	svc, prices := newPriceEnv()
	err := svc.AddPrice(context.Background(), &domain.User{ID: 7}, &domain.Pricebook{
		ProductID: 1, ProductCost: 5000, FinalPrice: 4000, ValidFrom: time.Date(2026, 11, 24, 0, 0, 0, 0, testLoc),
	})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) || ve.Fields["final_price"] == "" {
		t.Fatalf("expected final_price error, got %v", err)
	}
	if len(prices.created) != 0 {
		t.Error("nothing must be stored")
	}
}

func TestAddPriceUnknownProduct(t *testing.T) {
	svc, _ := newPriceEnv()
	err := svc.AddPrice(context.Background(), &domain.User{ID: 7}, &domain.Pricebook{
		ProductID: 99, ProductCost: 1000, FinalPrice: 2000, ValidFrom: time.Date(2026, 11, 24, 0, 0, 0, 0, testLoc),
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
