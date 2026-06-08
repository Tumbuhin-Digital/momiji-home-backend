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
	LineItems           LineItemsGroup `json:"line_items"`
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

type AddTrackingRequest struct {
	TrackingNumber string `json:"tracking_number" validate:"required"`
	TrackingURL    string `json:"tracking_url" validate:"required,url"`
}

type OrderQuery struct {
	Page   int    `query:"page"`
	Limit  int    `query:"limit"`
	Search string `query:"search"`
	Status string `query:"status"` // expected format could be financial/fulfillment depending on need, but the contract says 'status'
}
