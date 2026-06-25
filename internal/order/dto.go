package order

type GuestInfo struct {
	FirstName string `json:"first_name" validate:"required"`
	LastName  string `json:"last_name" validate:"required"`
	Email     string `json:"email" validate:"required,email"`
}

type CreateOrderRequest struct {
	ShippingMethod string     `json:"shipping_method,omitempty"`
	ShippingTitle  string     `json:"shipping_title,omitempty"`
	ShippingPrice  string     `json:"shipping_price,omitempty"`
	GuestInfo      *GuestInfo `json:"guest_info,omitempty"`
}

type LineItemsGroup struct {
	ShipReady []OrderItemDetail `json:"ship_ready"`
	PreOrder  []OrderItemDetail `json:"pre_order"`
}

type CustomerDTO struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone,omitempty"`
}

type AddressDTO struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2,omitempty"`
	City      string `json:"city"`
	Province  string `json:"province"`
	Country   string `json:"country"`
	Zip       string `json:"zip"`
	Phone     string `json:"phone,omitempty"`
}

type OrderResponse struct {
	ID                  string         `json:"id"`
	OrderNumber         string         `json:"order_number"`
	OrderDate           string         `json:"order_date"`
	Customer            *CustomerDTO   `json:"customer"`
	ShippingAddress     *AddressDTO    `json:"shipping_address"`
	ShopifyCheckoutURL  string         `json:"shopify_checkout_url,omitempty"`
	ShopifyDraftInvoice string         `json:"shopify_draft_invoice_url,omitempty"`
	TotalPrice          string         `json:"total_price"`
	AggregateStatus     string         `json:"aggregate_status"` // deprecated
	FinancialStatus     string         `json:"financial_status"`
	FulfillmentStatus   string         `json:"fulfillment_status"`
	TotalShipReady      string         `json:"total_ship_ready"`
	TotalDepositPaid    string         `json:"total_deposit_paid"`
	TotalBalanceDue     string         `json:"total_balance_due"`
	TotalChargedNow     string         `json:"total_charged_now"`
	Currency            string         `json:"currency"`
	LineItems           LineItemsGroup        `json:"line_items"`
	PreorderShipment    *PreorderShipmentDTO  `json:"preorder_shipment,omitempty"`
	ShippingMethod      string                `json:"shipping_method,omitempty"`
	Fulfillments        []FulfillmentDTO      `json:"fulfillments,omitempty"`
}

type OrderItemDetail struct {
	ID              string  `json:"id"`
	VariantID       string  `json:"variant_id"`
	Type            string  `json:"type"`
	Quantity        int     `json:"quantity"`
	ItemStatus      string  `json:"item_status"`
	DpAmount        *string `json:"dp_amount,omitempty"`
	FinalAmount     *string `json:"final_amount,omitempty"`
	Title           string  `json:"title"`
	UnitPrice       *string `json:"unit_price,omitempty"`
	AmountCharged   *string `json:"amount_charged,omitempty"`
	BalanceDue      *string `json:"balance_due,omitempty"`
	FulfillmentStep int     `json:"fulfillment_step"`
	ItemsReceived   int     `json:"items_received"`
	ImageSrc        string  `json:"image_src"`
	TrackingNumber    *string `json:"tracking_number,omitempty"`
	TrackingURL       *string `json:"tracking_url,omitempty"`
	TrackingCompany   *string `json:"tracking_company,omitempty"`
	TrackingLastEvent *string `json:"tracking_last_event,omitempty"`
	SKU               string  `json:"sku,omitempty"`
	WeightKg          float64 `json:"weight_kg,omitempty"`
	WidthCm           float64 `json:"width_cm,omitempty"`
	HeightCm          float64 `json:"height_cm,omitempty"`
	DepthCm           float64 `json:"depth_cm,omitempty"`
	RemainingQuantity int     `json:"remaining_quantity,omitempty"`
}

type PreorderShipmentDTO struct {
	EstimatedShipping  *string          `json:"estimated_shipping,omitempty"`
	FinalShippingPrice *string          `json:"final_shipping_price,omitempty"`
	ShippingNotes      *string          `json:"shipping_notes,omitempty"`
	CreditAmount       *string          `json:"credit_amount,omitempty"`
	TotalBoxes         int              `json:"total_boxes"`
	TotalWeightLb      *string          `json:"total_weight_lb,omitempty"`
	InvoiceSentAt      *string          `json:"invoice_sent_at,omitempty"`
	Packing            []PackingItemDTO `json:"packing,omitempty"`
}

type PackingItemDTO struct {
	LineItemID string `json:"line_item_id"`
	BoxCount   int    `json:"box_count"`
	IsNested   bool   `json:"is_nested"`
}

type CalculatePreorderShippingRequest struct {
	Packing []PackingItemDTO `json:"packing" validate:"required,min=1,dive"`
}

type CalculatePreorderShippingResponse struct {
	EstimatedShipping string           `json:"estimated_shipping"`
	TotalBoxes        int              `json:"total_boxes"`
	TotalWeightLb     string           `json:"total_weight_lb"`
	Packing           []PackingItemDTO `json:"packing"`
	ServiceCode       string           `json:"service_code"`
	Currency          string           `json:"currency"`
}

type UpdatePreorderShippingRequest struct {
	FinalShippingPrice float64          `json:"final_shipping_price" validate:"required,min=0"`
	ShippingNotes      string           `json:"shipping_notes"`
	Packing            []PackingItemDTO `json:"packing,omitempty"`
}

type AcceptOrderRequest struct {
	FulfillmentType string `json:"fulfillment_type" validate:"required"`
}

type CancelOrderRequest struct {
	FulfillmentType string `json:"fulfillment_type" validate:"required"`
	Reason          string `json:"reason" validate:"required"`
}

type UpdateStepRequest struct {
	FulfillmentStep int `json:"fulfillment_step" validate:"required,min=1,max=5"`
}

type UpdateReceivedItem struct {
	ItemID        string `json:"item_id" validate:"required,uuid"`
	ItemsReceived int    `json:"items_received" validate:"required,min=0"`
}

type UpdateReceivedRequest struct {
	Items []UpdateReceivedItem `json:"items" validate:"required,min=1,dive"`
}

type AddTrackingRequest struct {
	ItemIDs        []string `json:"item_ids" validate:"required,min=1"`
	TrackingNumber string   `json:"tracking_number" validate:"required"`
	TrackingURL    string   `json:"tracking_url" validate:"required,url"`
}

type OrderQuery struct {
	Page   int    `query:"page"`
	Limit  int    `query:"limit"`
	Search string `query:"search"`
	Status string `query:"status"` // expected format could be financial/fulfillment depending on need, but the contract says 'status'
}

type CreateFulfillmentItemRequest struct {
	LineItemID string `json:"line_item_id" validate:"required,uuid"`
	Quantity   int    `json:"quantity" validate:"required,min=1"`
}

type CreateFulfillmentRequest struct {
	Items           []CreateFulfillmentItemRequest `json:"items" validate:"required,min=1,dive"`
	TrackingNumber  string                         `json:"tracking_number" validate:"required"`
	TrackingCompany string                         `json:"tracking_company" validate:"required"`
	TrackingURL     string                         `json:"tracking_url,omitempty"`
	NotifyCustomer  bool                           `json:"notify_customer"`
}

type FulfillmentLineItemDTO struct {
	LineItemID string `json:"line_item_id"`
	Title      string `json:"title"`
	Quantity   int    `json:"quantity"`
	ImageSrc   string `json:"image_src,omitempty"`
	UnitPrice  string `json:"unit_price,omitempty"`
}

type FulfillmentDTO struct {
	ID              string                   `json:"id"`
	DisplayID       string                   `json:"display_id"`
	SequenceNumber  int                      `json:"sequence_number"`
	TrackingNumber  string                   `json:"tracking_number,omitempty"`
	TrackingURL     string                   `json:"tracking_url,omitempty"`
	TrackingCompany string                   `json:"tracking_company,omitempty"`
	ShipmentStatus  string                   `json:"shipment_status,omitempty"`
	Status          string                   `json:"status"`
	FulfilledAt     string                   `json:"fulfilled_at,omitempty"`
	DeliveredAt     string                   `json:"delivered_at,omitempty"`
	LineItems       []FulfillmentLineItemDTO `json:"line_items"`
}
