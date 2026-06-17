package cart

type GuestSessionResponse struct {
	SessionID string `json:"session_id"`
	ExpiresAt string `json:"expires_at"`
}

type CartResponse struct {
	SessionID string         `json:"session_id,omitempty"`
	ShipReady []CartItem     `json:"ship_ready"`
	PreOrder  []CartItem     `json:"pre_order"`
	Summary   CartSummaryDTO `json:"summary"`
}

type CartItem struct {
	ID                string `json:"id"`
	VariantID         string `json:"variant_id"`
	Title             string `json:"title"`
	ImageSrc          string `json:"image_src"`
	Quantity          int    `json:"quantity"`
	InventoryQuantity int    `json:"inventory_quantity"`
	UnitPrice         string `json:"unit_price"`
	DepositAmount     string `json:"deposit_amount,omitempty"`
	BalanceDue        string `json:"balance_due,omitempty"`
	Subtotal          string  `json:"subtotal"`
	Weight            float64 `json:"weight,omitempty"`
	WeightUnit        string  `json:"weight_unit,omitempty"`
	Length            float64 `json:"length,omitempty"`
	Width             float64 `json:"width,omitempty"`
	Height            float64 `json:"height,omitempty"`
}

type CartSummaryDTO struct {
	TotalShipReady  string `json:"total_ship_ready,omitempty"`
	TotalPreOrder   string `json:"total_pre_order,omitempty"`
	TotalDeposit    string `json:"total_deposit,omitempty"`
	TotalBalanceDue string `json:"total_balance_due,omitempty"`
	TotalChargedNow string `json:"total_charged_now"`
	Currency        string `json:"currency"`
}

type CartItemRequest struct {
	VariantID string `json:"variant_id" validate:"required"`
	Quantity  int    `json:"quantity" validate:"required,min=1"`
}

type UpdateCartItemRequest struct {
	Quantity int `json:"quantity" validate:"min=0"`
}

type MergeCartRequest struct {
	GuestSessionID string `json:"guest_session_id" validate:"required"`
}

type SetVariantQuantityRequest struct {
	VariantID     string `json:"variant_id" validate:"required"`
	TotalQuantity int    `json:"total_quantity" validate:"min=0"`
}
