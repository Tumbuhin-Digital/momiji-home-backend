package order

import (
	"context"
	"time"
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

	Items []OrderItem `gorm:"foreignKey:OrderID"`
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
	TrackingNumber   *string
	FulfillmentStep  int
	ItemsReceived    int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (OrderItem) TableName() string {
	return "order_line_items"
}

type Store interface {
	CreateOrder(ctx context.Context, order *Order) error
	GetOrder(ctx context.Context, orderID, customerID string) (*Order, error)
	GetOrdersByCustomer(ctx context.Context, customerID string) ([]Order, error)
	UpdateOrderStatus(ctx context.Context, orderID, financialStatus, fulfillmentStatus string) error
	UpdateOrderItemStep(ctx context.Context, itemID string, step int) error
	UpdateOrderItemReceived(ctx context.Context, itemID string, count int) error
}
