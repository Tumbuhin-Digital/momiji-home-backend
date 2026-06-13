package dashboard

import (
	"context"
	"time"
)

type DashboardRawStats struct {
	TotalProducts        int
	AvailableStockCount  int
	AvailableStockDelta  int
	OrdersInProgress     int
	OrdersInProgressDelta int
	PreOrdersCount       int
}

type RawRecentOrder struct {
	OrderNumber       string
	CustomerFirstName *string
	CustomerLastName  *string
	AggregateStatus   string
	CreatedAt         time.Time
	HasPreorderItems  bool
	PreviewItemTitle  *string
	PreviewItemQty    int
}

type RawMonthlyRevenue struct {
	Month   float64
	Revenue float64
}

type Store interface {
	GetDashboardStats(ctx context.Context) (*DashboardRawStats, error)
	GetRecentOrders(ctx context.Context) ([]RawRecentOrder, error)
	GetTotalRevenueThisMonth(ctx context.Context) (float64, error)
	GetMonthlyRevenue(ctx context.Context) ([]RawMonthlyRevenue, error)
}
