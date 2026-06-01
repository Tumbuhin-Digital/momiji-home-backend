package product

import (
	"context"
	"time"
)

type Product struct {
	ID          string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ShopifyID   string `gorm:"uniqueIndex"`
	Handle      string
	Title       string
	Description string
	Vendor      string
	ProductType string
	Tags        string
	Status      string
	BodyHTML    string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	Variants []ProductVariant `gorm:"foreignKey:ProductID"`
	Images   []ProductImage   `gorm:"foreignKey:ProductID"`
}

type ProductImage struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ProductID string
	ShopifyID string `gorm:"uniqueIndex"`
	Src       string
	Alt       string
	Position  int
}

type ProductVariant struct {
	ID                 string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ProductID          string
	ShopifyVariantID   string `gorm:"uniqueIndex"`
	Title              string
	SKU                string
	Price              float64
	ImageSrc           string
	RetailPrice        *float64
	WSPrice            *float64
	FulfillmentType    string
	PreorderBatchLabel     *string
	ExpectedShipDate       *time.Time
	InventoryQuantity      int
	ShopifyInventoryItemID string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Store interface {
	GetProducts(ctx context.Context, query ProductQuery) ([]Product, int64, error)
	GetVariantByShopifyID(ctx context.Context, shopifyVariantID string) (*ProductVariant, error)
	GetVariantByInventoryItemID(ctx context.Context, itemID string) (*ProductVariant, error)
	GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error)
	UpsertProduct(ctx context.Context, product *Product) error
	UpsertVariant(ctx context.Context, variant *ProductVariant) error
	UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64, retailPrice *float64) error
	GetProductByID(ctx context.Context, productID string) (*Product, error)
	GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error)
	UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) error
	UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) error
}
