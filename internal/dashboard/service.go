package dashboard

import (
	"context"
	"fmt"
)

type DashboardService interface {
	GetSummary(ctx context.Context) (*DashboardSummaryResponse, error)
}

type service struct {
	store Store
}

func NewDashboardService(store Store) DashboardService {
	return &service{store: store}
}

func (s *service) GetSummary(ctx context.Context) (*DashboardSummaryResponse, error) {
	stats, err := s.store.GetDashboardStats(ctx)
	if err != nil {
		return nil, err
	}

	rawOrders, err := s.store.GetRecentOrders(ctx)
	if err != nil {
		return nil, err
	}

	totalRev, err := s.store.GetTotalRevenueThisMonth(ctx)
	if err != nil {
		return nil, err
	}

	rawMonthly, err := s.store.GetMonthlyRevenue(ctx)
	if err != nil {
		return nil, err
	}

	// Format recent orders
	var recentOrders []RecentOrder
	for _, o := range rawOrders {
		customerName := ""
		if o.CustomerFirstName != nil {
			customerName = *o.CustomerFirstName
		}
		if o.CustomerLastName != nil {
			if customerName != "" {
				customerName += " "
			}
			customerName += *o.CustomerLastName
		}
		if customerName == "" {
			customerName = "Guest"
		}

		itemsPreview := ""
		if o.PreviewItemTitle != nil {
			itemsPreview = fmt.Sprintf("%s (%dpcs)", *o.PreviewItemTitle, o.PreviewItemQty)
		}

		status, statusLabel := mapOrderStatus(o.AggregateStatus, o.HasPreorderItems)

		recentOrders = append(recentOrders, RecentOrder{
			OrderNumber:  o.OrderNumber,
			CustomerName: customerName,
			ItemsPreview: itemsPreview,
			Status:       status,
			StatusLabel:  statusLabel,
		})
	}
	if recentOrders == nil {
		recentOrders = []RecentOrder{}
	}

	// Format monthly revenue
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	monthlyRev := make([]MonthlyRevenue, 12)
	for i, m := range months {
		monthlyRev[i] = MonthlyRevenue{Month: m, Revenue: 0}
	}
	for _, r := range rawMonthly {
		idx := int(r.Month) - 1
		if idx >= 0 && idx < 12 {
			monthlyRev[idx].Revenue = r.Revenue
		}
	}

	return &DashboardSummaryResponse{
		StatCards: StatCards{
			TotalProducts: stats.TotalProducts,
			AvailableStock: StatValue{
				Count:      stats.AvailableStockCount,
				DeltaToday: stats.AvailableStockDelta,
			},
			OrdersInProgress: StatValue{
				Count:      stats.OrdersInProgress,
				DeltaToday: stats.OrdersInProgressDelta,
			},
			PreOrders: PreOrderStat{
				Count:       stats.PreOrdersCount,
				StatusLabel: "Confirm Pending",
			},
		},
		RecentOrders: recentOrders,
		SalesReport: SalesReport{
			TotalRevenueThisMonth: totalRev,
			Currency:              "USD",
			MonthlyRevenue:        monthlyRev,
		},
	}, nil
}

func mapOrderStatus(aggregateStatus string, hasPreorderItems bool) (string, string) {
	switch aggregateStatus {
	case "pending_payment":
		return "new_order", "New Order"
	case "on_progress":
		if hasPreorderItems {
			return "pre_order", "Pre-Order"
		}
		return "order_confirm", "Order Confirm"
	case "refunded":
		return "refunded", "Refunded"
	case "cancelled":
		return "cancelled", "Cancelled"
	default:
		return aggregateStatus, aggregateStatus
	}
}
