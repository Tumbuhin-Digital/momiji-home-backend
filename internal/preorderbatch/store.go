package preorderbatch

import "context"

type Store interface {
	ListByVariantID(ctx context.Context, variantID string) ([]PreorderBatch, error)
	GetBatchByID(ctx context.Context, batchID string) (*PreorderBatch, error)
	GetCommittedAllocationsByOrderLineItemID(ctx context.Context, orderLineItemID string) ([]BatchAllocation, error)
	GetCommittedAllocationsByOrderLineItemIDs(ctx context.Context, orderLineItemIDs []string) ([]BatchAllocation, error)
	GetBatchesByIDs(ctx context.Context, batchIDs []string) ([]PreorderBatch, error)
	CreateBatch(ctx context.Context, batch *PreorderBatch) error
	UpdateBatch(ctx context.Context, batchID string, updates BatchUpdates) (*PreorderBatch, error)
	CloseBatch(ctx context.Context, batchID string) ([]PreorderBatch, error)
	CancelBatch(ctx context.Context, batchID string) (*PreorderBatch, error)
	ReorderBatch(ctx context.Context, batchID string, sequence int) (*PreorderBatch, error)
	CancelOpenBatchesByVariantID(ctx context.Context, variantID string) error
	AllocateToBatch(ctx context.Context, input AllocateInput) (*AllocateExecution, error)
	ReleaseAllocationsByOrderLineItemID(ctx context.Context, orderLineItemID string) error

	GetActiveLockedQtyForBatch(ctx context.Context, batchID string) (int, error)
	GetActiveLockedQtyByBatchIDs(ctx context.Context, batchIDs []string, excludeUserID, excludeSessionID *string) (map[string]int, error)
	CreateBatchLocks(ctx context.Context, locks []BatchLock) error
	DeleteBatchLocksBySession(ctx context.Context, userID, sessionID *string) error
	DeleteBatchLocksByCheckoutReference(ctx context.Context, checkoutReference string, userID, sessionID *string) error
	DeleteExpiredBatchLocks(ctx context.Context) error
}

type BatchUpdates struct {
	Name         *string
	QtyAllocated *int
}

type AllocateInput struct {
	VariantID          string
	ShopifyVariantID   string
	ProductTitle       string
	ImageURL           string
	Quantity           int
	OrderLineItemID    *string
	Commit             bool
	LockedQtyByBatchID map[string]int
}

type AllocateExecution struct {
	Batches      []PreorderBatch
	Allocations  []BatchAllocationLine
	UnlimitedQty int
	Depletion    *BatchDepletionEvent
}
