package preorder

import "time"

// SettlementResponse is the API representation of a preorder settlement.
type SettlementResponse struct {
	ID         string     `json:"id"`
	OrderID    string     `json:"order_id"`
	Status     string     `json:"status"`
	Amount     float64    `json:"amount"`
	InvoicedAt *time.Time `json:"invoiced_at,omitempty"`
	PaidAt     *time.Time `json:"paid_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ListSettlementsQuery holds query parameters for the list endpoint.
type ListSettlementsQuery struct {
	Status string `query:"status"`
	Page   int    `query:"page"`
	Limit  int    `query:"limit"`
}
