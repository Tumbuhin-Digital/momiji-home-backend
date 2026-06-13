package dashboard

type StatValue struct {
	Count      int `json:"count"`
	DeltaToday int `json:"delta_today,omitempty"`
}

type PreOrderStat struct {
	Count       int    `json:"count"`
	StatusLabel string `json:"status_label"`
}

type StatCards struct {
	TotalProducts    int          `json:"total_products"`
	AvailableStock   StatValue    `json:"available_stock"`
	OrdersInProgress StatValue    `json:"orders_in_progress"`
	PreOrders        PreOrderStat `json:"pre_orders"`
}

type RecentOrder struct {
	OrderNumber  string `json:"order_number"`
	CustomerName string `json:"customer_name"`
	ItemsPreview string `json:"items_preview"`
	Status       string `json:"status"`
	StatusLabel  string `json:"status_label"`
}

type MonthlyRevenue struct {
	Month   string  `json:"month"`
	Revenue float64 `json:"revenue"`
}

type SalesReport struct {
	TotalRevenueThisMonth float64          `json:"total_revenue_this_month"`
	Currency              string           `json:"currency"`
	MonthlyRevenue        []MonthlyRevenue `json:"monthly_revenue"`
}

type DashboardSummaryResponse struct {
	StatCards    StatCards     `json:"stat_cards"`
	RecentOrders []RecentOrder `json:"recent_orders"`
	SalesReport  SalesReport   `json:"sales_report"`
}
