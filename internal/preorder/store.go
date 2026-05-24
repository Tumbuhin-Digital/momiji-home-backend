package preorder

import (
	"context"
	"time"
)

// Settlement represents a pre-order payment settlement.
// One settlement is created per order that contains pre_order items.
// State machine: pending → invoiced → paid
type Settlement struct {
	ID         string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrderID    string     `gorm:"not null"`
	Status     string     `gorm:"not null;default:'pending'"` // pending | invoiced | paid
	Amount     float64    `gorm:"not null"`
	InvoicedAt *time.Time
	PaidAt     *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (Settlement) TableName() string {
	return "preorder_settlements"
}

// SettlementFilter is used for listing settlements with optional filters.
type SettlementFilter struct {
	Status string
	Page   int
	Limit  int
}

// PreorderStore defines the data-access contract for settlements.
type PreorderStore interface {
	CreateSettlement(ctx context.Context, s *Settlement) error
	GetSettlementByID(ctx context.Context, id string) (*Settlement, error)
	ListSettlements(ctx context.Context, filter SettlementFilter) ([]Settlement, int64, error)
	UpdateSettlementStatus(ctx context.Context, id, status string, ts *time.Time) error
	AllSettlementsPaid(ctx context.Context, orderID string) (bool, error)
}
