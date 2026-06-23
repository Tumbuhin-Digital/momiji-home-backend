package order

import "time"

// PreorderShipment stores pre-order fulfillment shipping state per order.
type PreorderShipment struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrderID            string     `gorm:"not null;uniqueIndex"`
	EstimatedShipping  *float64
	FinalShippingPrice *float64
	ShippingNotes      *string
	CreditAmount       float64    `gorm:"not null;default:0"`
	TotalBoxes         int        `gorm:"not null;default:0"`
	TotalWeightLb      *float64
	InvoiceSentAt      *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time

	PackingItems []PreorderPackingItem `gorm:"foreignKey:PreorderShipmentID"`
}

func (PreorderShipment) TableName() string {
	return "preorder_shipments"
}

// PreorderPackingItem stores per-line-item box packing for pre-order fulfillment.
type PreorderPackingItem struct {
	ID                   string  `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	PreorderShipmentID   string  `gorm:"not null"`
	OrderLineItemID      string  `gorm:"not null;uniqueIndex"`
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
