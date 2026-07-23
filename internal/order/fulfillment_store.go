package order

import (
	"context"
	"time"
)

type FulfillmentOrder struct {
	ID                         string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrderID                    string
	ShopifyFulfillmentOrderID  string `gorm:"uniqueIndex"`
	Status                     string
	AssignedLocationName       *string
	CreatedAt                  time.Time
	UpdatedAt                  time.Time

	LineItems []FulfillmentOrderLineItem `gorm:"foreignKey:FulfillmentOrderID"`
}

func (FulfillmentOrder) TableName() string { return "fulfillment_orders" }

type FulfillmentOrderLineItem struct {
	ID                                  string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	FulfillmentOrderID                  string
	OrderLineItemID                     string
	ShopifyFulfillmentOrderLineItemID   string `gorm:"uniqueIndex"`
	TotalQuantity                       int
	RemainingQuantity                   int
	CreatedAt                           time.Time
	UpdatedAt                           time.Time
}

func (FulfillmentOrderLineItem) TableName() string { return "fulfillment_order_line_items" }

type Fulfillment struct {
	ID                   string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	OrderID              string
	BatchID              *string `gorm:"type:uuid;index"` // nil = unbatched / legacy
	ShopifyFulfillmentID *string `gorm:"uniqueIndex"`
	SequenceNumber       int
	TrackingNumber       *string
	TrackingURL          *string
	TrackingCompany      *string
	ShipmentStatus       *string
	Status               string
	NotifyCustomer       bool
	FulfilledAt          *time.Time
	DeliveredAt          *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time

	LineItems []FulfillmentLineItem `gorm:"foreignKey:FulfillmentID"`
}

func (Fulfillment) TableName() string { return "fulfillments" }

type FulfillmentLineItem struct {
	ID              string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	FulfillmentID   string
	OrderLineItemID string
	Quantity        int
	CreatedAt       time.Time
}

func (FulfillmentLineItem) TableName() string { return "fulfillment_line_items" }

type ShopifyWebhookEvent struct {
	WebhookID   string `gorm:"primaryKey"`
	Topic       string
	ProcessedAt time.Time
}

func (ShopifyWebhookEvent) TableName() string { return "shopify_webhook_events" }

// SyncedFulfillmentOrderLineItem is used when upserting FO data from Shopify GraphQL.
type SyncedFulfillmentOrderLineItem struct {
	ShopifyFulfillmentOrderLineItemID string
	OrderLineItemID                   string
	TotalQuantity                     int
	RemainingQuantity                 int
}

type FulfillmentStore interface {
	UpsertFulfillmentOrder(ctx context.Context, orderID, shopifyFOID, status string, locationName *string, lineItems []SyncedFulfillmentOrderLineItem) error
	GetFulfillmentOrdersByOrderID(ctx context.Context, orderID string) ([]FulfillmentOrder, error)
	GetFOLIByOrderLineItemIDs(ctx context.Context, orderID string, lineItemIDs []string) ([]FulfillmentOrderLineItem, error)
	DecrementFOLIRemaining(ctx context.Context, foliID string, qty int) error

	CreateFulfillment(ctx context.Context, f *Fulfillment) error
	UpsertFulfillmentByShopifyID(ctx context.Context, f *Fulfillment, lineItems []FulfillmentLineItem) error
	GetFulfillmentsByOrderID(ctx context.Context, orderID string) ([]Fulfillment, error)
	GetFulfillmentByID(ctx context.Context, fulfillmentID string) (*Fulfillment, error)
	GetNextFulfillmentSequence(ctx context.Context, orderID string) (int, error)
	MarkFulfillmentDelivered(ctx context.Context, fulfillmentID string, deliveredAt time.Time) error

	RecordWebhookEvent(ctx context.Context, webhookID, topic string) (alreadyProcessed bool, err error)
	IsWebhookProcessed(ctx context.Context, webhookID string) (bool, error)
	SaveWebhookEvent(ctx context.Context, webhookID, topic string) error
}
