package preorder

import (
	"context"
	"time"
)

// Settlement represents a pre-order payment settlement.
// One settlement is created per order_line_item that is pre_order.
// State machine: pending → invoiced → paid
type Settlement struct {
	ID              string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrderLineItemID string     `gorm:"not null"`
	OrderID         string     `gorm:"->"` // derived via join if needed
	Status          string     `gorm:"not null;default:'pending'"` // pending | invoiced | paid
	BalanceAmount   float64    `gorm:"not null"`
	DueDate              *time.Time
	InvoicedAt           *time.Time
	PaidAt               *time.Time
	ShopifyInvoiceURL    *string `gorm:"column:shopify_invoice_url"`
	ShopifyDraftOrderID  *string `gorm:"column:shopify_draft_order_id"`
	CreatedAt            time.Time
	UpdatedAt            time.Time

	// Joined fields (read-only)
	Title         string `gorm:"->"`
	CustomerEmail string `gorm:"->"`
	CustomerName  string `gorm:"->"`
}

func (Settlement) TableName() string {
	return "preorder_settlements"
}

// PreorderRow represents the joined data for the list endpoint.
type PreorderRow struct {
	ID               string
	OrderID          string
	OrderNumber      string
	CustomerEmail    string
	CustomerName     string
	OrderLineItemID  string
	Title            string
	Quantity         int
	BalanceAmount    float64
	BatchLabel       string
	SettlementStatus string
	DueDate             *time.Time
	ShopifyOrderID      *string
	ShopifyInvoiceURL   *string
	ShopifyDraftOrderID *string
	CreatedAt           time.Time
}

// SettlementFilter is used for listing settlements with optional filters.
type SettlementFilter struct {
	Status     string
	BatchLabel string
	StartDate  *time.Time
	EndDate    *time.Time
	Page       int
	Limit      int
}

// PreorderStore defines the data-access contract for settlements.
type PreorderStore interface {
	CreateSettlement(ctx context.Context, s *Settlement) error
	GetSettlementByID(ctx context.Context, id string) (*Settlement, error)
	GetSettlementByOrderLineItemID(ctx context.Context, itemID string) (*Settlement, error)
	ListSettlements(ctx context.Context, filter SettlementFilter) ([]PreorderRow, int64, error)
	GetAllSettlementsForExport(ctx context.Context, filter SettlementFilter) ([]PreorderRow, error)
	UpdateSettlementStatus(ctx context.Context, id, status string, ts *time.Time) error
	MarkSettlementsInvoiced(ctx context.Context, ids []string, draftOrderID, invoiceURL string, invoicedAt time.Time) error
	AllSettlementsPaid(ctx context.Context, orderID string) (bool, error)
	GetSettlementsForReminder(ctx context.Context, daysSinceInvoiced int) ([]PreorderRow, error)
}
