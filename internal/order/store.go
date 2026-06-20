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
	ShippingMethod      *string
	ShippingCost        float64
	TotalShipReady      float64
	TotalDepositPaid    float64
	TotalBalanceDue     float64
	TotalChargedNow     float64
	Currency            string
	Note                *string
	CreatedAt           time.Time
	UpdatedAt           time.Time

	Items           []OrderItem       `gorm:"foreignKey:OrderID"`
	Customer        *customer.Customer `gorm:"foreignKey:CustomerID"`
	ShippingAddress *customer.Address  `gorm:"foreignKey:ShippingAddressID"`
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
	UpdateItemStatusByType(ctx context.Context, orderID string, itemType string, status string) error
	UpdateItemStatusByID(ctx context.Context, itemID string, status string) error
	UpdateItemStepByType(ctx context.Context, orderID string, itemType string, step int) error
	UpdateOrderItemStep(ctx context.Context, itemID string, step int) error
	UpdateOrderItemTracking(ctx context.Context, itemID, trackingNumber, trackingURL, trackingCompany, trackingLastEvent string, shippedAt *time.Time) error
	UpdateOrderItemReceived(ctx context.Context, itemID string, count int) error
}
