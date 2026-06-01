package checkout

import "github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"

type ShippingMethod struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	EstimatedArrival string `json:"estimated_arrival"`
	Cost             string `json:"cost"`
}

type ShippingMethodsResponse struct {
	Methods  []ShippingMethod `json:"methods"`
	Currency string           `json:"currency"`
}

type CalculateShippingRequest struct {
	ShippingMethod string `json:"shipping_method" validate:"required"`
	AddressID      int    `json:"address_id,omitempty"`
	Country        string `json:"country,omitempty"`
	Province       string `json:"province,omitempty"`
	Zip            string `json:"zip,omitempty"`
}

type CheckoutSummaryRequest struct {
	ShippingMethod string `json:"shipping_method" validate:"required"`
	AddressID      int    `json:"address_id,omitempty"`
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
		ShipReadyTotal string `json:"ship_ready_total"`
		Shipping       string `json:"shipping"`
		PreorderDeposit string `json:"preorder_deposit"`
		Total          string `json:"total"`
	} `json:"due_now"`
	DueAugust struct {
		PreorderBalance  string `json:"preorder_balance"`
		ShippingPreorder string `json:"shipping_preorder"`
		Total            string `json:"total"`
	} `json:"due_august"`
	Currency string `json:"currency"`
}

type InitiateCheckoutRequest struct {
	ShippingMethod string `json:"shipping_method" validate:"required"`
	AddressID      int    `json:"address_id,omitempty"`
	Email          string `json:"email,omitempty"`
}

type InitiateCheckoutResponse struct {
	CheckoutUrl string `json:"checkout_url"`
}
