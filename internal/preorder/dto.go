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

// PreorderListItemResponse represents a rich row matching the API contract.
type PreorderListItemResponse struct {
	OrderID          string  `json:"order_id"`
	OrderNumber      string  `json:"order_number"`
	CustomerEmail    string  `json:"customer_email"`
	ItemID           string  `json:"item_id"`
	Title            string  `json:"title"`
	Quantity         int     `json:"quantity"`
	BalanceDue       string  `json:"balance_due"`
	BatchLabel       string  `json:"batch_label"`
	SettlementStatus string  `json:"settlement_status"`
	DueDate          string  `json:"due_date,omitempty"`
}

// ListSettlementsQuery holds query parameters for the list endpoint.
type ListSettlementsQuery struct {
	Status     string `query:"status"`
	BatchLabel string `query:"batch_label"`
	Page       int    `query:"page"`
	Limit      int    `query:"limit"`
}
