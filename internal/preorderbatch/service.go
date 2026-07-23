package preorderbatch

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preordercustomtext"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"gorm.io/gorm"
)

type Service interface {
	ListVariantBatches(ctx context.Context, shopifyVariantID string) (*ListVariantBatchesResponse, error)
	CreateBatch(ctx context.Context, shopifyVariantID string, req CreateBatchRequest) (*BatchDTO, error)
	UpdateBatch(ctx context.Context, batchID string, req UpdateBatchRequest) (*BatchDTO, error)
	CloseBatch(ctx context.Context, batchID string) (*ListVariantBatchesResponse, error)
	CancelBatch(ctx context.Context, batchID string) (*BatchDTO, error)
	ReorderBatch(ctx context.Context, batchID string, sequence int) (*BatchDTO, error)
	AllocateToBatch(ctx context.Context, shopifyVariantID string, qty int, ref AllocationRef) (*AllocateResult, error)
	PreviewAllocation(ctx context.Context, shopifyVariantID string, qty int, userID, sessionID *string) (*AllocateResult, error)
	CancelOpenBatchesForVariant(ctx context.Context, shopifyVariantID string) error
	ReleaseAllocationsByOrderLineItemID(ctx context.Context, orderLineItemID string) error
	GetCommittedAllocationsByOrderLineItemIDs(ctx context.Context, orderLineItemIDs []string) ([]BatchAllocation, error)
	GetBatchesByIDs(ctx context.Context, batchIDs []string) ([]PreorderBatch, error)
}

type service struct {
	store             Store
	productStore      product.Store
	customTextService preordercustomtext.Service
}

func NewService(store Store, productStore product.Store, customTextService preordercustomtext.Service) Service {
	return &service{
		store:             store,
		productStore:      productStore,
		customTextService: customTextService,
	}
}

func (s *service) ListVariantBatches(ctx context.Context, shopifyVariantID string) (*ListVariantBatchesResponse, error) {
	variant, err := s.requirePreorderVariant(ctx, shopifyVariantID)
	if err != nil {
		return nil, err
	}

	batches, err := s.store.ListByVariantID(ctx, variant.ID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	return mapListResponse(batches), nil
}

func (s *service) CreateBatch(ctx context.Context, shopifyVariantID string, req CreateBatchRequest) (*BatchDTO, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apierror.New(http.StatusBadRequest, "validation_error", "name is required")
	}
	if req.QtyAllocated < 1 {
		return nil, apierror.New(http.StatusBadRequest, "validation_error", "qty_allocated must be at least 1")
	}

	variant, err := s.requirePreorderVariant(ctx, shopifyVariantID)
	if err != nil {
		return nil, err
	}

	batch := &PreorderBatch{
		VariantID:        variant.ID,
		ShopifyProductID: variant.Product.ShopifyID,
		Name:             name,
		QtyAllocated:     req.QtyAllocated,
	}

	if err := s.store.CreateBatch(ctx, batch); err != nil {
		return nil, mapStoreErr(err)
	}
	if batch.Status == StatusActive {
		if err := s.syncVariantLabel(ctx, variant.ShopifyVariantID, batch.Name); err != nil {
			return nil, err
		}
	}
	dto := mapBatchDTO(*batch)
	return &dto, nil
}

func (s *service) UpdateBatch(ctx context.Context, batchID string, req UpdateBatchRequest) (*BatchDTO, error) {
	if req.Name == nil && req.QtyAllocated == nil {
		return nil, apierror.New(http.StatusBadRequest, "validation_error", "at least one field is required")
	}

	updates := BatchUpdates{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, apierror.New(http.StatusBadRequest, "validation_error", "name is required")
		}
		updates.Name = &name
	}
	if req.QtyAllocated != nil {
		if *req.QtyAllocated < 1 {
			return nil, apierror.New(http.StatusBadRequest, "validation_error", "qty_allocated must be at least 1")
		}
		updates.QtyAllocated = req.QtyAllocated
	}

	current, err := s.store.GetBatchByID(ctx, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if current == nil {
		return nil, apierror.ErrNotFound
	}
	if updates.QtyAllocated != nil && *updates.QtyAllocated < current.QtySold {
		return nil, apierror.New(http.StatusUnprocessableEntity, "invalid_qty_allocated", "qty_allocated cannot be lower than qty_sold")
	}

	batch, err := s.store.UpdateBatch(ctx, batchID, updates)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if batch.Status == StatusActive && updates.Name != nil {
		if err := s.syncActiveBatchLabelByVariantID(ctx, batch.VariantID); err != nil {
			return nil, err
		}
	}

	dto := mapBatchDTO(*batch)
	return &dto, nil
}

func (s *service) CloseBatch(ctx context.Context, batchID string) (*ListVariantBatchesResponse, error) {
	batches, err := s.store.CloseBatch(ctx, batchID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	if len(batches) == 0 {
		return nil, apierror.ErrNotFound
	}
	if err := s.syncLabelForBatches(ctx, batches); err != nil {
		return nil, err
	}
	return mapListResponse(batches), nil
}

func (s *service) CancelBatch(ctx context.Context, batchID string) (*BatchDTO, error) {
	batch, err := s.store.CancelBatch(ctx, batchID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	dto := mapBatchDTO(*batch)
	return &dto, nil
}

func (s *service) ReorderBatch(ctx context.Context, batchID string, sequence int) (*BatchDTO, error) {
	if sequence < 1 {
		return nil, apierror.New(http.StatusBadRequest, "validation_error", "sequence must be at least 1")
	}

	batch, err := s.store.ReorderBatch(ctx, batchID, sequence)
	if err != nil {
		return nil, mapStoreErr(err)
	}

	batches, err := s.store.ListByVariantID(ctx, batch.VariantID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if err := s.syncLabelForBatches(ctx, batches); err != nil {
		return nil, err
	}

	dto := mapBatchDTO(*batch)
	return &dto, nil
}

func (s *service) AllocateToBatch(ctx context.Context, shopifyVariantID string, qty int, ref AllocationRef) (*AllocateResult, error) {
	return s.runAllocation(ctx, shopifyVariantID, qty, ref, true, nil, nil)
}

func (s *service) PreviewAllocation(ctx context.Context, shopifyVariantID string, qty int, userID, sessionID *string) (*AllocateResult, error) {
	return s.runAllocation(ctx, shopifyVariantID, qty, AllocationRef{}, false, userID, sessionID)
}

func (s *service) runAllocation(ctx context.Context, shopifyVariantID string, qty int, ref AllocationRef, commit bool, userID, sessionID *string) (*AllocateResult, error) {
	if qty < 1 {
		return nil, apierror.New(http.StatusBadRequest, "validation_error", "qty must be at least 1")
	}
	variant, err := s.requirePreorderVariant(ctx, shopifyVariantID)
	if err != nil {
		return nil, err
	}

	batches, err := s.store.ListByVariantID(ctx, variant.ID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	batchIDs := make([]string, 0, len(batches))
	for _, batch := range batches {
		if batch.Status == StatusActive || batch.Status == StatusQueued {
			batchIDs = append(batchIDs, batch.ID)
		}
	}
	lockedByBatch, err := s.store.GetActiveLockedQtyByBatchIDs(ctx, batchIDs, userID, sessionID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	productTitle := ""
	imageURL := variant.ImageSrc
	if variant.Product != nil {
		productTitle = variant.Product.Title
	}

	execution, err := s.store.AllocateToBatch(ctx, AllocateInput{
		VariantID:          variant.ID,
		ShopifyVariantID:   variant.ShopifyVariantID,
		ProductTitle:       productTitle,
		ImageURL:           imageURL,
		Quantity:           qty,
		OrderLineItemID:    ref.OrderLineItemID,
		Commit:             commit,
		LockedQtyByBatchID: lockedByBatch,
	})
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if commit {
		if err := s.syncLabelForBatches(ctx, execution.Batches); err != nil {
			return nil, err
		}
	}
	return &AllocateResult{
		Allocations:  execution.Allocations,
		UnlimitedQty: execution.UnlimitedQty,
		Depletion:    execution.Depletion,
	}, nil
}

func (s *service) CancelOpenBatchesForVariant(ctx context.Context, shopifyVariantID string) error {
	variant, err := s.productStore.GetVariantByShopifyID(ctx, shopifyVariantID)
	if err != nil {
		return apierror.ErrInternal
	}
	if variant == nil {
		return apierror.ErrNotFound
	}
	if err := s.store.CancelOpenBatchesByVariantID(ctx, variant.ID); err != nil {
		return apierror.ErrInternal
	}
	return s.syncVariantLabel(ctx, shopifyVariantID, "")
}

func (s *service) ReleaseAllocationsByOrderLineItemID(ctx context.Context, orderLineItemID string) error {
	if strings.TrimSpace(orderLineItemID) == "" {
		return nil
	}
	if err := s.store.ReleaseAllocationsByOrderLineItemID(ctx, orderLineItemID); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func (s *service) GetCommittedAllocationsByOrderLineItemIDs(ctx context.Context, orderLineItemIDs []string) ([]BatchAllocation, error) {
	return s.store.GetCommittedAllocationsByOrderLineItemIDs(ctx, orderLineItemIDs)
}

func (s *service) GetBatchesByIDs(ctx context.Context, batchIDs []string) ([]PreorderBatch, error) {
	return s.store.GetBatchesByIDs(ctx, batchIDs)
}

// normalizeShopifyVariantID accepts full GraphQL GIDs or bare numeric IDs.
// Numeric IDs are preferred in URL paths because proxies like Vercel reject %2F.
func normalizeShopifyVariantID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "://") {
		return id
	}
	if _, err := strconv.ParseUint(id, 10, 64); err == nil {
		return "gid://shopify/ProductVariant/" + id
	}
	return id
}

func (s *service) requirePreorderVariant(ctx context.Context, shopifyVariantID string) (*product.ProductVariant, error) {
	shopifyVariantID = normalizeShopifyVariantID(shopifyVariantID)
	variant, err := s.productStore.GetVariantByShopifyID(ctx, shopifyVariantID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if variant == nil {
		return nil, apierror.ErrNotFound
	}
	if variant.Product == nil {
		prod, err := s.productStore.GetProductByID(ctx, variant.ProductID)
		if err != nil {
			return nil, apierror.ErrInternal
		}
		if prod != nil {
			variant.Product = prod
		}
	}
	if variant.FulfillmentType != string(product.FulfillmentTypePreOrder) {
		return nil, apierror.New(http.StatusBadRequest, "invalid_variant_status", "variant must be pre_order")
	}
	return variant, nil
}

func (s *service) syncLabelForBatches(ctx context.Context, batches []PreorderBatch) error {
	if len(batches) == 0 {
		return nil
	}
	var active *PreorderBatch
	for idx := range batches {
		if batches[idx].Status == StatusActive {
			active = &batches[idx]
			break
		}
	}
	variantID := batches[0].VariantID
	variant, err := s.getVariantByInternalID(ctx, variantID)
	if err != nil {
		return err
	}
	if active == nil {
		return s.syncVariantLabel(ctx, variant.ShopifyVariantID, "")
	}
	return s.syncVariantLabel(ctx, variant.ShopifyVariantID, active.Name)
}

func (s *service) syncActiveBatchLabelByVariantID(ctx context.Context, variantID string) error {
	batches, err := s.store.ListByVariantID(ctx, variantID)
	if err != nil {
		return apierror.ErrInternal
	}
	return s.syncLabelForBatches(ctx, batches)
}

func (s *service) getVariantByInternalID(ctx context.Context, variantID string) (*product.ProductVariant, error) {
	variants, err := s.productStore.GetAllVariants(ctx)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	for _, variant := range variants {
		if variant.ID == variantID {
			return &variant, nil
		}
	}
	return nil, apierror.ErrNotFound
}

func (s *service) syncVariantLabel(ctx context.Context, shopifyVariantID string, label string) error {
	if strings.TrimSpace(label) != "" {
		if _, err := s.customTextService.EnsureByLabel(ctx, label); err != nil {
			return apierror.ErrInternal
		}
	}
	if err := s.productStore.UpdateSingleVariantBatchLabel(ctx, shopifyVariantID, strings.TrimSpace(label)); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func mapStoreErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		return apierror.ErrNotFound
	case errors.Is(err, gorm.ErrDuplicatedKey):
		return apierror.New(http.StatusConflict, "duplicate_batch_name", "batch name must be unique per variant")
	case errors.Is(err, gorm.ErrInvalidData):
		return apierror.New(http.StatusBadRequest, "invalid_batch_state", "batch cannot be modified in its current state")
	default:
		return apierror.ErrInternal
	}
}

func mapBatchDTO(batch PreorderBatch) BatchDTO {
	var closedAt *string
	if batch.ClosedAt != nil {
		value := batch.ClosedAt.UTC().Format(time.RFC3339)
		closedAt = &value
	}
	return BatchDTO{
		ID:           batch.ID,
		Name:         batch.Name,
		QtyAllocated: batch.QtyAllocated,
		QtySold:      batch.QtySold,
		QtyRemaining: batch.QtyRemaining(),
		Sequence:     batch.Sequence,
		Status:       batch.Status,
		CreatedAt:    batch.CreatedAt.UTC().Format(time.RFC3339),
		ClosedAt:     closedAt,
	}
}

func mapListResponse(batches []PreorderBatch) *ListVariantBatchesResponse {
	items := make([]BatchDTO, 0, len(batches))
	var activeBatchID *string
	for _, batch := range batches {
		items = append(items, mapBatchDTO(batch))
		if batch.Status == StatusActive {
			activeBatchID = &batch.ID
		}
	}
	return &ListVariantBatchesResponse{
		Batches: items,
		Summary: BatchSummaryDTO{
			TotalBatches:         len(items),
			ActiveBatchID:        activeBatchID,
			HasUnlimitedFallback: true,
		},
	}
}
