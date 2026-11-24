package service

import (
	"context"
	"time"

	"patylook/internal/core/port"
)

const (
	lowStockThreshold = 5
	recentOrdersLimit = 8
)

type DashboardService struct {
	orders   port.OrderRepository
	products port.ProductRepository
	now      Clock
}

var _ port.DashboardService = (*DashboardService)(nil)

func NewDashboardService(o port.OrderRepository, p port.ProductRepository, now Clock) *DashboardService {
	return &DashboardService{orders: o, products: p, now: now}
}

// Load summarizes the current month.
func (s *DashboardService) Load(ctx context.Context) (*port.Dashboard, error) {
	now := s.now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	summary, err := s.orders.Summary(ctx, start)
	if err != nil {
		return nil, err
	}
	low, err := s.products.CountLowStock(ctx, lowStockThreshold)
	if err != nil {
		return nil, err
	}
	recent, err := s.orders.Recent(ctx, recentOrdersLimit)
	if err != nil {
		return nil, err
	}
	return &port.Dashboard{Summary: summary, LowStock: low, RecentOrders: recent, PeriodStart: start}, nil
}
