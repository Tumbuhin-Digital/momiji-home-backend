package cart

import (
	"context"
	"time"
)

type Cart struct {
	ID                string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	SessionID         *string
	UserID            *string
	ShopifyCheckoutID *string
	Status            string
	ExpiresAt         time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time

	Items []CartItemModel `gorm:"foreignKey:CartID"`
}

type CartItemModel struct {
	ID               string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	CartID           string
	ShopifyVariantID string
	FulfillmentType  string
	Quantity         int
	UnitPrice        float64
}

func (CartItemModel) TableName() string {
	return "cart_items"
}

type CartStore interface {
	GetCart(ctx context.Context, userID, sessionID *string) (*Cart, error)
	CreateCart(ctx context.Context, cart *Cart) error
	AddItem(ctx context.Context, item *CartItemModel) error
	GetVariantQtyInCart(ctx context.Context, cartID string, shopifyVariantID string) (int, error)
	UpdateItemQuantity(ctx context.Context, itemID string, quantity int) error
	RemoveItem(ctx context.Context, itemID string) error
	ClearCart(ctx context.Context, cartID string) error
	MergeCarts(ctx context.Context, sourceCartID, targetCartID string) error
}
