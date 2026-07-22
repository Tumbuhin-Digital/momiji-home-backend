package preorderbatch

type BatchDTO struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	QtyAllocated int     `json:"qty_allocated"`
	QtySold      int     `json:"qty_sold"`
	QtyRemaining int     `json:"qty_remaining"`
	Sequence     int     `json:"sequence"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	ClosedAt     *string `json:"closed_at"`
}

type BatchSummaryDTO struct {
	TotalBatches         int     `json:"total_batches"`
	ActiveBatchID        *string `json:"active_batch_id"`
	HasUnlimitedFallback bool    `json:"has_unlimited_fallback"`
}

type ListVariantBatchesResponse struct {
	Batches []BatchDTO      `json:"batches"`
	Summary BatchSummaryDTO `json:"summary"`
}

type CreateBatchRequest struct {
	Name         string `json:"name" validate:"required,max=128"`
	QtyAllocated int    `json:"qty_allocated" validate:"required,min=1"`
}

type UpdateBatchRequest struct {
	Name         *string `json:"name,omitempty" validate:"omitempty,max=128"`
	QtyAllocated *int    `json:"qty_allocated,omitempty" validate:"omitempty,min=1"`
}

type ReorderBatchRequest struct {
	Sequence int `json:"sequence" validate:"required,min=1"`
}

type BatchAllocationLine struct {
	BatchID   string `json:"batch_id"`
	BatchName string `json:"batch_name"`
	Quantity  int    `json:"quantity"`
}

type BatchDepletionEvent struct {
	VariantID       string  `json:"variant_id"`
	ClosedBatchName string  `json:"closed_batch_name"`
	NextBatchName   *string `json:"next_batch_name"`
	ProductTitle    string  `json:"product_title"`
	ImageURL        string  `json:"image_url"`
}

type AllocationRef struct {
	OrderLineItemID *string
}

type AllocateResult struct {
	Allocations  []BatchAllocationLine `json:"allocations"`
	UnlimitedQty int                   `json:"unlimited_qty"`
	Depletion    *BatchDepletionEvent  `json:"depletion,omitempty"`
}
