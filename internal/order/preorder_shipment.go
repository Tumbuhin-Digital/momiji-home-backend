package order

import (
	"errors"
	"time"
)

// ErrPreorderShipmentPaid reports that a group's second payment was settled before a
// change to its invoice could be saved.
var ErrPreorderShipmentPaid = errors.New("preorder shipment invoice already paid")

// PreorderShipment stores pre-order fulfillment shipping state per order group.
// BatchID nil means the unbatched (regular) pre-order group.
type PreorderShipment struct {
	ID                 string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrderID            string     `gorm:"not null;index"`
	BatchID            *string    `gorm:"type:uuid;index"`
	EstimatedShipping  *float64
	FinalShippingPrice *float64
	// PrepaidShipping is the 50% already collected at checkout; settlement bills the rest.
	// Collected once per order, so it lands on the checkout-created row and acts as a pool
	// shared across batch groups — see AllocatePrepaidShipping.
	PrepaidShipping float64 `gorm:"not null;default:0"`
	// PrepaidApplied is how much of that pool this group's settlement invoice consumed.
	PrepaidApplied float64 `gorm:"not null;default:0"`
	ShippingNotes  *string
	CreditAmount       float64    `gorm:"not null;default:0"`
	TotalBoxes         int        `gorm:"not null;default:0"`
	TotalWeightLb      *float64
	WarehouseOrigin      string     `gorm:"not null;default:east"`
	RateCalculatedAt     *time.Time `gorm:"column:rate_calculated_at"`
	InvoiceSentAt        *time.Time
	ShopifyDraftOrderID  *string    `gorm:"column:shopify_draft_order_id"`
	InvoiceURL           *string    `gorm:"column:invoice_url"`
	InvoicePaidAt        *time.Time `gorm:"column:invoice_paid_at"`
	// BalanceMethod is how the second payment is billed: nil or BalanceMethodDraftOrder for
	// a separate draft-order invoice, BalanceMethodOrderEdit for an edit of the deposit order.
	// An order-edit group with no InvoiceSentAt has an edit in flight whose outcome is unknown.
	BalanceMethod *string `gorm:"column:balance_method"`
	// BalanceExpectedOutstanding is what the deposit order must owe once this group's edit is
	// committed; it identifies a committed edit whose save failed.
	BalanceExpectedOutstanding *float64 `gorm:"column:balance_expected_outstanding"`
	// ShopifyShippingLineID is the shipping line the edit added for the remaining shipping.
	ShopifyShippingLineID *string `gorm:"column:shopify_shipping_line_id"`
	// BalanceShippingDepositFolded is the upfront "Shipping (Pre-Order) - 50%" line the edit
	// removed and added back into ShopifyShippingLineID, which therefore shows the group's
	// full shipping. Zero when the upfront line was kept.
	BalanceShippingDepositFolded float64 `gorm:"column:balance_shipping_deposit_folded;not null;default:0"`
	CreatedAt                    time.Time
	UpdatedAt                    time.Time

	PackingItems []PreorderPackingItem `gorm:"foreignKey:PreorderShipmentID"`
}

func (PreorderShipment) TableName() string {
	return "preorder_shipments"
}

const (
	BalanceMethodDraftOrder = "draft_order"
	BalanceMethodOrderEdit  = "order_edit"
)

// IsOrderEditBalance reports whether this group's balance is billed by editing the deposit order.
func (s *PreorderShipment) IsOrderEditBalance() bool {
	return s != nil && s.BalanceMethod != nil && *s.BalanceMethod == BalanceMethodOrderEdit
}

// AwaitingOrderEditPayment reports whether this group's balance edit is committed and unpaid.
func (s *PreorderShipment) AwaitingOrderEditPayment() bool {
	return s.IsOrderEditBalance() && s.InvoiceSentAt != nil && s.InvoicePaidAt == nil
}

// PreorderBalanceLine is a real product line that a balance edit added to the deposit order.
// Lines added by an order edit cannot carry custom attributes, so this row is the only link
// from the Shopify line back to the app line item it bills.
type PreorderBalanceLine struct {
	ID                string  `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ShipmentID        string  `gorm:"column:shipment_id;not null"`
	OrderLineItemID   string  `gorm:"column:order_line_item_id;not null"`
	ShopifyLineItemID string  `gorm:"column:shopify_line_item_id;not null"`
	Quantity          int     `gorm:"not null"`
	UnitPrice         float64 `gorm:"column:unit_price;not null"`
	CreatedAt         time.Time
}

func (PreorderBalanceLine) TableName() string {
	return "preorder_balance_lines"
}

// OrderEditBalanceResult is what a committed balance edit is saved as.
type OrderEditBalanceResult struct {
	PaymentURL          string
	PrepaidApplied      float64
	ExpectedOutstanding float64
	ShippingLineID      string
	Lines               []PreorderBalanceLine
	SentAt              time.Time
}

// VariantStockRef identifies a variant's Shopify inventory item.
type VariantStockRef struct {
	ShopifyVariantID string
	InventoryItemID  string
	Tracked          bool
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
