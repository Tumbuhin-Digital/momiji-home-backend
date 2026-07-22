package preorderbatch

import "time"

const (
	StatusActive    = "active"
	StatusQueued    = "queued"
	StatusClosed    = "closed"
	StatusCancelled = "cancelled"
)

const (
	AllocationStatusCommitted = "committed"
	AllocationStatusReleased  = "released"
)

type PreorderBatch struct {
	ID               string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	VariantID        string     `gorm:"column:variant_id;not null"`
	ShopifyProductID string     `gorm:"column:shopify_product_id;not null"`
	Name             string     `gorm:"not null"`
	QtyAllocated     int        `gorm:"column:qty_allocated;not null"`
	QtySold          int        `gorm:"column:qty_sold;not null"`
	Sequence         int        `gorm:"not null"`
	Status           string     `gorm:"not null"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at"`
	ClosedAt         *time.Time `gorm:"column:closed_at"`
}

func (PreorderBatch) TableName() string {
	return "preorder_batches"
}

func (b PreorderBatch) QtyRemaining() int {
	remaining := b.QtyAllocated - b.QtySold
	if remaining < 0 {
		return 0
	}
	return remaining
}

type BatchAllocation struct {
	ID               string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	BatchID          string     `gorm:"column:batch_id;not null"`
	OrderLineItemID  *string    `gorm:"column:order_line_item_id"`
	ShopifyVariantID string     `gorm:"column:shopify_variant_id;not null"`
	Quantity         int        `gorm:"not null"`
	Status           string     `gorm:"not null"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	ReleasedAt       *time.Time `gorm:"column:released_at"`
}

func (BatchAllocation) TableName() string {
	return "preorder_batch_allocations"
}

type BatchLock struct {
	ID                string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	BatchID           string    `gorm:"column:batch_id;not null"`
	ShopifyVariantID  string    `gorm:"column:shopify_variant_id;not null"`
	Quantity          int       `gorm:"not null"`
	SessionID         *string   `gorm:"column:session_id"`
	UserID            *string   `gorm:"column:user_id"`
	CheckoutReference *string   `gorm:"column:checkout_reference"`
	ExpiresAt         time.Time `gorm:"column:expires_at;not null"`
	CreatedAt         time.Time `gorm:"column:created_at"`
}

func (BatchLock) TableName() string {
	return "preorder_batch_locks"
}
