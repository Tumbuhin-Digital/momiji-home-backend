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
	ShopifyOrderID   *string
	TrackingNumber   *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Store interface {
	CreateOrder(ctx context.Context, order *Order) error
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	GetOrdersByCustomer(ctx context.Context, customerID string) ([]Order, error)
	UpdateOrderStatus(ctx context.Context, orderID string, status string) error
}
