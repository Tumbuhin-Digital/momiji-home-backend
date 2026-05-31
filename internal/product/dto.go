package product

// FulfillmentType represents how a product is fulfilled.
type FulfillmentType string

const (
	FulfillmentTypeShipReady FulfillmentType = "ship_ready"
	FulfillmentTypePreOrder  FulfillmentType = "pre_order"
)

type VariantDTO struct {
	ID              string          `json:"id"`
	Title           string          `json:"title"`
	ImageSrc        string          `json:"image_src"`
	RetailPrice     string          `json:"retail_price"`
	WSPrice         string          `json:"ws_price"`
	FulfillmentType   FulfillmentType `json:"fulfillment_type"`
	InventoryQuantity int             `json:"inventory_quantity"`
}

type ProductImageDTO struct {
	ID  string `json:"id"`
	Src string `json:"src"`
}

type ProductDTO struct {
	ID          string            `json:"id"`
	ShopifyID   string            `json:"shopify_id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Variants    []VariantDTO      `json:"variants"`
	Images      []ProductImageDTO `json:"images"`
}

type ProductQuery struct {
	Page            int    `query:"page"`
	Limit           int    `query:"limit"`
	Search          string `query:"search"`
	Sort            string `query:"sort"`
	FulfillmentType string `query:"fulfillment_type"`
}
