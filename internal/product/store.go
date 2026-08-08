package product

import (
	"context"
	"time"
)

const (
	ProductOriginShopifySync = "shopify_sync"
	ProductOriginCustom      = "custom"

	ProductStatusCreating = "creating"
	ProductStatusFailed   = "failed"
	ProductStatusUnlisted = "unlisted"
	ProductStatusActive   = "active"
	ProductStatusDeleted  = "deleted"

	CustomLinkStateAwaitingSKU = "awaiting_sku"
	CustomLinkStateLinked      = "linked"
)

type Product struct {
	ID                   string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ShopifyID            string `gorm:"uniqueIndex"`
	Handle               string
	Title                string
	Description          string
	Vendor               string
	ProductType          string
	Tags                 string
	Status               string
	BodyHTML             string
	Origin               string  `gorm:"default:shopify_sync"`
	InternalNote         *string
	CreateIdempotencyKey *string `gorm:"uniqueIndex"`
	CreatedAt            time.Time
	UpdatedAt            time.Time

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
	ID                     string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ProductID              string
	Product                *Product `gorm:"foreignKey:ProductID"`
	ShopifyVariantID       string   `gorm:"uniqueIndex" json:"shopify_variant_id"`
	Title                  string
	SKU                    string
	Price                  float64
	ImageSrc               string
	RetailPrice            *float64
	WSPrice                *float64
	WeightKg               float64
	WidthCm                float64
	HeightCm               float64
	DepthCm                float64
	FulfillmentType        string
	PreorderBatchLabel     *string
	ExpectedShipDate       *time.Time
	InventoryQuantity      int
	ShopifyInventoryItemID string
	IsLtl                  bool
	CustomLinkState        *string
	InventoryTracked       bool `gorm:"default:true"`
	CreatedAt              time.Time
	UpdatedAt              time.Time
	BatchSummary           *VariantBatchSummary `gorm:"-"`
}

type VariantBatchSummary struct {
	TotalCount           int
	ActiveCount          int
	QueuedCount          int
	HasBatches           bool
	ActiveBatchID        *string
	ActiveBatchName      *string
	ActiveBatchRemaining *int
	NextBatchName        *string
	NextBatchRemaining   *int
	MaxBatchOrderableQty *int
}

type Store interface {
	GetProducts(ctx context.Context, query ProductQuery) ([]Product, int64, error)
	ListShopifyProductIDs(ctx context.Context) ([]string, error)
	GetVariantByShopifyID(ctx context.Context, shopifyVariantID string) (*ProductVariant, error)
	GetVariantByInventoryItemID(ctx context.Context, itemID string) (*ProductVariant, error)
	GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error)
	UpsertProduct(ctx context.Context, product *Product) error
	MarkProductDeletedByShopifyID(ctx context.Context, shopifyID string) error
	UpsertVariant(ctx context.Context, variant *ProductVariant) error
	UpdateInventoryQuantity(ctx context.Context, shopifyVariantID string, quantity int) error
	UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64) error
	UpdateVariantLtl(ctx context.Context, variantID string, isLtl bool) error
	GetProductByID(ctx context.Context, productID string) (*Product, error)
	IsVariantFromActiveProduct(ctx context.Context, shopifyVariantID string) (bool, error)
	GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error)
	GetAllVariants(ctx context.Context) ([]ProductVariant, error)
	GetBatchSummariesByVariantIDs(ctx context.Context, variantIDs []string) (map[string]VariantBatchSummary, error)
	UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) error
	UpdateVariantFulfillmentType(ctx context.Context, variantID string, fulfillmentType string) error
	UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) error
	UpdateSingleVariantBatchLabel(ctx context.Context, shopifyVariantID string, batchLabel string) error
	UpsertProductImages(ctx context.Context, productID string, images []ProductImage) error
	BulkUpdateVariantDimensions(ctx context.Context, inputs []DimensionUpdateInput) (BulkUpdateDimensionsResult, error)
	GetProductByIdempotencyKey(ctx context.Context, key string) (*Product, error)
	CreateCustomProductStub(ctx context.Context, product *Product) error
	FinalizeCustomProduct(ctx context.Context, productID string, product *Product, variants []ProductVariant, images []ProductImage) error
	MarkCustomProductFailed(ctx context.Context, productID string) error
	DeleteProductByID(ctx context.Context, productID string) error
	MarkCustomVariantLinked(ctx context.Context, shopifyVariantID, sku string) error
}

type DimensionUpdateInput struct {
	ShopifyVariantID string
	WidthCm          float64
	HeightCm         float64
	DepthCm          float64
	// UpdateDimensions when true writes width/height/depth (including zeros).
	UpdateDimensions bool
	// WeightKg nil means skip weight update for this row.
	WeightKg *float64
}

type PackagingImportError struct {
	Row       int    `json:"row"`
	VariantID string `json:"variant_id,omitempty"`
	Field     string `json:"field,omitempty"`
	Message   string `json:"message"`
}

type BulkUpdateDimensionsResult struct {
	UpdatedCount       int
	WeightUpdatedCount int
	NotFoundIDs        []string
}
