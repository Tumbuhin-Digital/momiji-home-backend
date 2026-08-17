package order

import (
	"context"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
)

type Order struct {
	ID                  string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ShopifyOrderID      *string
	ShopifyDraftOrderID *string
	CustomerID          string
	TotalPrice          float64
	AggregateStatus     string
	OrderNumber         string `gorm:"uniqueIndex"`
	FinancialStatus     string
	FulfillmentStatus   string
	ShippingAddressID   *string
	BillingAddressID    *string
	ShippingMethod      *string
	ShippingCost        float64
	TotalShipReady      float64
	TotalDepositPaid    float64
	TotalBalanceDue     float64
	TotalChargedNow     float64
	Currency            string
	Note                *string
	ShipTogether        bool
	HoldUntilBatch      *string
	CreatedAt           time.Time
	UpdatedAt           time.Time

	Items           []OrderItem        `gorm:"foreignKey:OrderID"`
	Customer        *customer.Customer `gorm:"foreignKey:CustomerID"`
	ShippingAddress *customer.Address  `gorm:"foreignKey:ShippingAddressID"`
	BillingAddress  *customer.Address  `gorm:"foreignKey:BillingAddressID"`
}

type OrderItem struct {
	ID               string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrderID          string
	ShopifyVariantID string
	Type             string
	Quantity         int
	ItemStatus       string
	DpAmount         *float64
	FinalAmount      *float64
	Title            *string
	UnitPrice        *float64
	AmountCharged    *float64
	BalanceDue       *float64
	ShopifyOrderID   *string
	TrackingNumber    *string
	TrackingURL       *string
	TrackingCompany   *string
	TrackingLastEvent *string
	ShippedAt        *time.Time
	FulfillmentStep  int
	ItemsReceived    int
	CreatedAt        time.Time
	UpdatedAt        time.Time
	ImageSrc         string `gorm:"-"`
}

func (OrderItem) TableName() string {
	return "order_line_items"
}

type Store interface {
	CreateOrder(ctx context.Context, order *Order) error
	GetOrder(ctx context.Context, orderID, customerID string) (*Order, error)
	GetOrderByShopifyID(ctx context.Context, shopifyOrderID string) (*Order, error)
	GetOrdersByCustomer(ctx context.Context, customerID string, query OrderQuery) ([]Order, int64, error)
	GetAllOrdersForExport(ctx context.Context, query OrderQuery) ([]Order, error)
	UpdateOrderStatus(ctx context.Context, orderID string, aggregateStatus, financialStatus, fulfillmentStatus string) error
	UpdateOrderHoldUntilBatch(ctx context.Context, orderID string, holdUntilBatch *string) error
	UpdateItemStatusByType(ctx context.Context, orderID string, itemType string, status string) error
	UpdateItemStatusByID(ctx context.Context, itemID string, status string) error
	UpdateItemStepByType(ctx context.Context, orderID string, itemType string, step int) error
	UpdateOrderItemStep(ctx context.Context, itemID string, step int) error
	UpdateOrderItemTracking(ctx context.Context, itemID, trackingNumber, trackingURL, trackingCompany, trackingLastEvent, itemStatus string, fulfillmentStep int, shippedAt *time.Time) error
	UpdateOrderItemReceived(ctx context.Context, itemID string, count int, fulfillmentStep int) error

	GetPreorderShipment(ctx context.Context, orderID string) (*PreorderShipment, error)
	GetPreorderShipments(ctx context.Context, orderID string) ([]PreorderShipment, error)
	GetPreorderShipmentByBatch(ctx context.Context, orderID string, batchID *string) (*PreorderShipment, error)
	GetPreorderShipmentByID(ctx context.Context, shipmentID string) (*PreorderShipment, error)
	UpsertPreorderShipment(ctx context.Context, shipment *PreorderShipment, packing []PreorderPackingItem) error
	UpdatePreorderShipping(ctx context.Context, orderID string, finalPrice float64, notes string, creditAmount float64) error
	UpdatePreorderShippingByShipmentID(ctx context.Context, shipmentID string, finalPrice float64, notes string, creditAmount float64) error
	MarkPreorderInvoiceSent(ctx context.Context, orderID string, sentAt time.Time) error
	MarkPreorderShipmentInvoiceSent(ctx context.Context, shipmentID string, draftOrderID, invoiceURL string, sentAt time.Time, prepaidApplied float64) error
	MarkPreorderShipmentInvoicePaid(ctx context.Context, shipmentID string, paidAt time.Time) error
	GetPreorderShipmentByDraftOrderID(ctx context.Context, draftOrderID string) (*PreorderShipment, error)
	HasAnyShipmentInvoiceForOrder(ctx context.Context, orderID string) (bool, error)
	GetVariantDimensions(ctx context.Context, shopifyVariantIDs []string) (map[string]VariantDimensions, error)
	GetUSZipStateAbbr(ctx context.Context, zip string) (string, bool)

	FulfillmentStore
}
