package preordercustomtext

type PreorderCustomTextDTO struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	UsageCount  int64  `json:"usage_count"`
}

type CreatePreorderCustomTextRequest struct {
	Label string `json:"label" validate:"required"`
}

type DeletePreorderCustomTextResponse struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	UsageCount  int64  `json:"usage_count"`
}

type ListQuery struct {
	Search string `query:"search"`
}
