package settings

type CheckoutNotesResponse struct {
	DueNowNote   string `json:"due_now_note"`
	DueLaterNote string `json:"due_later_note"`
}

type UpdateSettingsRequest struct {
	DueNowNote   string `json:"due_now_note"`
	DueLaterNote string `json:"due_later_note"`
}
