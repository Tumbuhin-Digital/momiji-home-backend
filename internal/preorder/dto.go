package preorder

import "time"

// SettlementResponse is the API representation of a preorder settlement.
type SettlementResponse struct {
	ID              string     `json:"id"`
	OrderLineItemID string     `json:"order_line_item_id"`
	OrderID         string     `json:"order_id,omitempty"`
	Status          string     `json:"status"`
	BalanceAmount   float64    `json:"balance_amount"`
	DueDate         *time.Time `json:"due_date,omitempty"`
	InvoicedAt      *time.Time `json:"invoiced_at,omitempty"`
	PaidAt          *time.Time `json:"paid_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// ListSettlementsQuery holds query parameters for the list endpoint.
type ListSettlementsQuery struct {
	Status string `query:"status"`
	Page   int    `query:"page"`
	Limit  int    `query:"limit"`
}
