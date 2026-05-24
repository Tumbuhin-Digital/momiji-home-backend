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
	FulfillmentType FulfillmentType `json:"fulfillment_type"`
}

type ProductDetailDTO struct {
	ID          string `json:"id"`
	ShopifyID   string `json:"shopify_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}
