package order

import "time"

// PreorderShipment stores pre-order fulfillment shipping state per order group.
// BatchID nil means the unbatched (regular) pre-order group.
type PreorderShipment struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrderID            string     `gorm:"not null;index"`
	BatchID            *string    `gorm:"type:uuid;index"`
	EstimatedShipping  *float64
	FinalShippingPrice *float64
	ShippingNotes      *string
	CreditAmount       float64    `gorm:"not null;default:0"`
	TotalBoxes         int        `gorm:"not null;default:0"`
	TotalWeightLb      *float64
	WarehouseOrigin      string     `gorm:"not null;default:east"`
	RateCalculatedAt     *time.Time `gorm:"column:rate_calculated_at"`
	InvoiceSentAt        *time.Time
	ShopifyDraftOrderID  *string    `gorm:"column:shopify_draft_order_id"`
	InvoiceURL           *string    `gorm:"column:invoice_url"`
	InvoicePaidAt        *time.Time `gorm:"column:invoice_paid_at"`
	CreatedAt            time.Time
	UpdatedAt            time.Time

	PackingItems []PreorderPackingItem `gorm:"foreignKey:PreorderShipmentID"`
}

func (PreorderShipment) TableName() string {
	return "preorder_shipments"
}

// PreorderPackingItem stores per-line-item box packing for a pre-order shipment group.
// Quantity is the slice qty for this shipment (may be less than the full line qty).
type PreorderPackingItem struct {
	ID                   string  `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	PreorderShipmentID   string  `gorm:"not null"`
	OrderLineItemID      string  `gorm:"not null;index"`
	Quantity             int     `gorm:"not null;default:0"`
	BoxCount             int     `gorm:"not null;default:0"`
	IsNested             bool    `gorm:"not null;default:false"`
	NestedInLineItemID   *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (PreorderPackingItem) TableName() string {
	return "preorder_packing_items"
}

// VariantDimensions holds product variant physical attributes for shipping.
type VariantDimensions struct {
	ShopifyVariantID string
	SKU              string
	WeightKg         float64
	WidthCm          float64
	HeightCm         float64
	DepthCm          float64
}
