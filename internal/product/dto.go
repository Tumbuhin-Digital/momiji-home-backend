package product

// FulfillmentType represents how a product is fulfilled.
type FulfillmentType string

const (
	FulfillmentTypeShipReady FulfillmentType = "ship_ready"
	FulfillmentTypePreOrder  FulfillmentType = "pre_order"
	FulfillmentTypeInactive  FulfillmentType = "inactive"
)

type VariantDTO struct {
	ID                string          `json:"id"`
	Title             string          `json:"title"`
	SKU               *string         `json:"sku"`
	ImageSrc          string          `json:"image_src"`
	RetailPrice       string          `json:"retail_price"`
	WSPrice           string          `json:"ws_price"`
	FulfillmentType   FulfillmentType `json:"fulfillment_type"`
	InventoryQuantity int             `json:"inventory_quantity"`
	WeightKg          float64         `json:"weight_kg"`
	LengthCm          float64         `json:"length_cm"`
	WidthCm           float64         `json:"width_cm"`
	HeightCm          float64         `json:"height_cm"`
}

type ProductImageDTO struct {
	ID       string `json:"id"`
	Src      string `json:"src"`
	Alt      string `json:"alt"`
	Position int    `json:"position"`
}

type ProductDTO struct {
	ID                 string            `json:"id"`
	ShopifyID          string            `json:"shopify_id"`
	Handle             string            `json:"handle"`
	Title              string            `json:"title"`
	Description        string            `json:"description"`
	Vendor             string            `json:"vendor"`
	ProductType        string            `json:"product_type"`
	Tags               string            `json:"tags"`
	Status             string            `json:"status"`
	PreorderBatchLabel *string           `json:"preorder_batch_label"`
	ExpectedShipDate   *string           `json:"expected_ship_date"`
	BodyHTML           string            `json:"body_html"`
	Variants           []VariantDTO      `json:"variants"`
	Images             []ProductImageDTO `json:"images"`
}

type ProductQuery struct {
	Page                       int    `query:"page"`
	Limit                      int    `query:"limit"`
	Search                     string `query:"search"`
	Sort                       string `query:"sort"`
	FulfillmentType            string `query:"fulfillment_type"`
	ExcludeInactive            bool   `query:"-"`
	FilterVariantsInResponse   bool   `query:"-"`
}

type UpdateProductStatusRequest struct {
	FulfillmentType string `json:"fulfillment_type" validate:"required"`
}

type UpdateVariantBatchLabelRequest struct {
	PreorderBatchLabel string  `json:"preorder_batch_label" validate:"required"`
	ExpectedShipDate   *string `json:"expected_ship_date"`
}

type UpdateVariantPriceRequest struct {
	VariantID   string   `json:"variant_id" validate:"required"`
	WSPrice     *float64 `json:"ws_price" validate:"omitempty,min=0"`
}

type UpdateVariantStatusRequest struct {
	VariantID       string `json:"variant_id" validate:"required"`
	FulfillmentType string `json:"fulfillment_type" validate:"required"`
}
