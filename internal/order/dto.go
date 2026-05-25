package order

type GuestInfo struct {
	FirstName string `json:"first_name" validate:"required"`
	LastName  string `json:"last_name" validate:"required"`
	Email     string `json:"email" validate:"required,email"`
}

type CreateOrderRequest struct {
	ShippingMethod string     `json:"shipping_method" validate:"required"`
	GuestInfo      *GuestInfo `json:"guest_info,omitempty"`
}

type LineItemsGroup struct {
	ShipReady []OrderItemDetail `json:"ship_ready"`
	PreOrder  []OrderItemDetail `json:"pre_order"`
}

type OrderResponse struct {
	ID                  string         `json:"id"`
	ShopifyCheckoutURL  string         `json:"shopify_checkout_url,omitempty"`
	ShopifyDraftInvoice string         `json:"shopify_draft_invoice_url,omitempty"`
	TotalPrice          string         `json:"total_price"`
	AggregateStatus     string         `json:"aggregate_status"`
	LineItems           LineItemsGroup `json:"line_items"`
}

type OrderItemDetail struct {
	ID               string  `json:"id"`
	VariantID        string  `json:"variant_id"`
	Type             string  `json:"type"`
	Quantity         int     `json:"quantity"`
	ItemStatus       string  `json:"item_status"`
	FulfillmentStep  int     `json:"fulfillment_step"`
	ItemsReceived    int     `json:"items_received"`
	DpAmount         *string `json:"dp_amount,omitempty"`
	FinalAmount      *string `json:"final_amount,omitempty"`
}

type AcceptOrderRequest struct {
	FulfillmentType string `json:"fulfillment_type" validate:"required"`
}

type CancelOrderRequest struct {
	FulfillmentType string `json:"fulfillment_type" validate:"required"`
	Reason          string `json:"reason" validate:"required"`
}

type UpdateStepRequest struct {
	FulfillmentStep int `json:"fulfillment_step" validate:"required,min=1,max=4"`
}

type UpdateReceivedRequest struct {
	ItemsReceived int `json:"items_received" validate:"required,min=0"`
}
