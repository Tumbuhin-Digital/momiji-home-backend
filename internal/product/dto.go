package product

// FulfillmentType represents how a product is fulfilled.
type FulfillmentType string

const (
	FulfillmentTypeShipReady FulfillmentType = "ship_ready"
	FulfillmentTypePreOrder  FulfillmentType = "pre_order"
	FulfillmentTypeInactive  FulfillmentType = "inactive"
)

type VariantDTO struct {
	ID                 string                  `json:"id"`
	Title              string                  `json:"title"`
	SKU                *string                 `json:"sku"`
	ImageSrc           string                  `json:"image_src"`
	RetailPrice        string                  `json:"retail_price"`
	WSPrice            string                  `json:"ws_price"`
	FulfillmentType    FulfillmentType         `json:"fulfillment_type"`
	InventoryQuantity  int                     `json:"inventory_quantity"`
	InventoryTracked   bool                    `json:"inventory_tracked"`
	CustomLinkState    *string                 `json:"custom_link_state,omitempty"`
	WeightKg           float64                 `json:"weight_kg"`
	LengthCm           float64                 `json:"length_cm"`
	WidthCm            float64                 `json:"width_cm"`
	HeightCm           float64                 `json:"height_cm"`
	PreorderBatchLabel *string                 `json:"preorder_batch_label"`
	IsLtl              bool                    `json:"is_ltl"`
	BatchSummary       *VariantBatchSummaryDTO `json:"batch_summary,omitempty"`
}

type VariantBatchSummaryDTO struct {
	TotalCount           int     `json:"total_count"`
	ActiveCount          int     `json:"active_count"`
	QueuedCount          int     `json:"queued_count"`
	HasBatches           bool    `json:"has_batches"`
	ActiveBatchID        *string `json:"active_batch_id,omitempty"`
	ActiveBatchName      *string `json:"active_batch_name,omitempty"`
	ActiveBatchRemaining *int    `json:"active_batch_remaining,omitempty"`
	NextBatchName        *string `json:"next_batch_name,omitempty"`
	NextBatchRemaining   *int    `json:"next_batch_remaining,omitempty"`
	MaxBatchOrderableQty *int    `json:"max_batch_orderable_qty,omitempty"`
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
	Origin             string            `json:"origin"`
	InternalNote       *string           `json:"internal_note,omitempty"`
	PreorderBatchLabel *string           `json:"preorder_batch_label"`
	ExpectedShipDate   *string           `json:"expected_ship_date"`
	BodyHTML           string            `json:"body_html"`
	Variants           []VariantDTO      `json:"variants"`
	Images             []ProductImageDTO `json:"images"`
}

type ProductQuery struct {
	Page                     int    `query:"page"`
	Limit                    int    `query:"limit"`
	Search                   string `query:"search"`
	Sort                     string `query:"sort"`
	FulfillmentType          string `query:"fulfillment_type"`
	Status                   string `query:"status"`
	ExcludeInactive          bool   `query:"-"`
	FilterVariantsInResponse bool   `query:"-"`
}

type UpdateProductStatusRequest struct {
	FulfillmentType    string `json:"fulfillment_type" validate:"required"`
	ConfirmBatchCancel bool   `json:"confirm_batch_cancel"`
}

type UpdateVariantBatchLabelRequest struct {
	VariantID          string `json:"variant_id" validate:"required"`
	PreorderBatchLabel string `json:"preorder_batch_label"`
}

type UpdateProductBatchLabelRequest struct {
	PreorderBatchLabel string  `json:"preorder_batch_label" validate:"required"`
	ExpectedShipDate   *string `json:"expected_ship_date"`
}

type UpdateVariantPriceRequest struct {
	VariantID string   `json:"variant_id" validate:"required"`
	WSPrice   *float64 `json:"ws_price" validate:"omitempty,min=0"`
}

type UpdateVariantStatusRequest struct {
	VariantID          string `json:"variant_id" validate:"required"`
	FulfillmentType    string `json:"fulfillment_type" validate:"required"`
	ConfirmBatchCancel bool   `json:"confirm_batch_cancel"`
}

type UpdateVariantLtlRequest struct {
	VariantID string `json:"variant_id" validate:"required"`
	IsLtl     bool   `json:"is_ltl"`
}

type LinkCustomVariantSKURequest struct {
	VariantID string `json:"variant_id" validate:"required"`
	SKU       string `json:"sku" validate:"required"`
}

type CreateCustomVariantRequest struct {
	Title    string  `json:"title" validate:"required"`
	RPPPrice float64 `json:"rpp_price" validate:"required,min=0"`
	WSPrice  float64 `json:"ws_price" validate:"required,min=0"`
}

type CreateCustomProductRequest struct {
	Title             string                       `json:"title" validate:"required"`
	InternalNote      *string                      `json:"internal_note"`
	IdempotencyKey    string                       `json:"idempotency_key" validate:"required"`
	ReferenceImageURL *string                      `json:"reference_image_url"`
	Variants          []CreateCustomVariantRequest `json:"variants" validate:"required,min=1,max=100,dive"`
}

type AddProductVariantsRequest struct {
	IdempotencyKey string                       `json:"idempotency_key" validate:"required"`
	Variants       []CreateCustomVariantRequest `json:"variants" validate:"required,min=1,max=100,dive"`
}
