package settings

type CheckoutNotesResponse struct {
	DueNowNote            string `json:"due_now_note"`
	DueLaterNote          string `json:"due_later_note"`
	PreorderShippingNote  string `json:"preorder_shipping_note"`
	StoreClosed           bool   `json:"store_closed"`
	StoreClosedMessage    string `json:"store_closed_message"`
}

type UpdateSettingsRequest struct {
	DueNowNote            string `json:"due_now_note"`
	DueLaterNote          string `json:"due_later_note"`
	PreorderShippingNote  string `json:"preorder_shipping_note"`
	StoreClosed           bool   `json:"store_closed"`
	StoreClosedMessage    string `json:"store_closed_message"`
}
