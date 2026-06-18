package checkout

import "github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"

type CheckoutSummaryRequest struct {
	ShippingMethod string `json:"shipping_method,omitempty"`
	AddressID      int    `json:"address_id,omitempty"`
	Zip            string `json:"zip,omitempty"`
	Country        string `json:"country,omitempty"`
}

type CheckoutSummaryResponse struct {
	ShipReady struct {
		Items    []cart.CartItem `json:"items"`
		Subtotal string          `json:"subtotal"`
	} `json:"ship_ready"`
	PreOrder struct {
		Items           []cart.CartItem `json:"items"`
		DepositSubtotal string          `json:"deposit_subtotal"`
		BalanceSubtotal string          `json:"balance_subtotal"`
	} `json:"pre_order"`
	Shipping struct {
		Method           string `json:"method"`
		Cost             string `json:"cost"`
		EstimatedArrival string `json:"estimated_arrival"`
	} `json:"shipping"`
	DueNow struct {
		ShipReadyTotal  string `json:"ship_ready_total"`
		Shipping        string `json:"shipping"`
		PreorderDeposit string `json:"preorder_deposit"`
		Total           string `json:"total"`
	} `json:"due_now"`
	DueAugust struct {
		PreorderBalance  string `json:"preorder_balance"`
		ShippingPreorder string `json:"shipping_preorder"`
		Total            string `json:"total"`
	} `json:"due_august"`
	Currency string `json:"currency"`
}

type InitiateCheckoutRequest struct {
	ShippingMethod string `json:"shipping_method,omitempty"`
	AddressID      int    `json:"address_id,omitempty"`
	Email          string `json:"email,omitempty"`
	FirstName      string `json:"first_name,omitempty"`
	LastName       string `json:"last_name,omitempty"`
	Address1       string `json:"address1,omitempty"`
	City           string `json:"city,omitempty"`
	State          string `json:"state,omitempty"`
	Zip            string `json:"zip,omitempty"`
	Country        string `json:"country,omitempty"`
	Phone          string `json:"phone,omitempty"`
}

type InitiateCheckoutResponse struct {
	CheckoutUrl       string `json:"checkout_url"`
	CheckoutReference string `json:"checkout_reference"`
}

type ShippingRatesRequest struct {
	Name     string `json:"name,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Address1 string `json:"address1,omitempty"`
	City     string `json:"city,omitempty"`
	State    string `json:"state,omitempty"`
	Zip      string `json:"zip" validate:"required"`
	Country  string `json:"country" validate:"required"`
}

type ValidateAddressRequest struct {
	Country string `json:"country" validate:"required"`
	State   string `json:"state" validate:"required"`
	City    string `json:"city" validate:"required"`
	Zip     string `json:"zip" validate:"required"`
}

type ShippingRateDTO struct {
	ServiceCode  string `json:"service_code"`
	Label        string `json:"label"`
	Cost         string `json:"cost"`
	Currency     string `json:"currency"`
	DeliveryDays *int   `json:"delivery_days,omitempty"`
}
