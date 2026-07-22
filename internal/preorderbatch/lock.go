package preorderbatch

import (
	"context"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

const batchLockTTL = 15 * time.Minute

type BatchLockRequest struct {
	ShopifyVariantID string
	Quantity         int
	ProductTitle     string
	ImageURL         string
}

type BatchLockService interface {
	AcquireBatchLocks(ctx context.Context, userID, sessionID *string, checkoutReference string, requests []BatchLockRequest) (time.Time, error)
	ReleaseBatchLocks(ctx context.Context, userID, sessionID *string) error
	ReleaseBatchLocksByCheckoutReference(ctx context.Context, checkoutReference string) error
	ReleaseBatchLocksForIdentity(ctx context.Context, userID, sessionID *string, checkoutReference *string) error
	CleanExpiredBatchLocks(ctx context.Context) error
}

type batchLockService struct {
	store          Store
	resolveVariant func(ctx context.Context, shopifyVariantID string) (string, error)
}

func NewBatchLockService(store Store, resolveVariant func(ctx context.Context, shopifyVariantID string) (string, error)) BatchLockService {
	return &batchLockService{
		store:          store,
		resolveVariant: resolveVariant,
	}
}

func (s *batchLockService) AcquireBatchLocks(
	ctx context.Context,
	userID, sessionID *string,
	checkoutReference string,
	requests []BatchLockRequest,
) (time.Time, error) {
	if len(requests) == 0 {
		return time.Time{}, nil
	}

	if err := s.store.DeleteBatchLocksBySession(ctx, userID, sessionID); err != nil {
		return time.Time{}, err
	}

	expiresAt := time.Now().Add(batchLockTTL)
	checkoutRef := checkoutReference
	var newLocks []BatchLock

	for _, req := range requests {
		if req.Quantity < 1 {
			continue
		}
		variantID, err := s.resolveVariant(ctx, req.ShopifyVariantID)
		if err != nil {
			return time.Time{}, err
		}
		if variantID == "" {
			return time.Time{}, apierror.ErrNotFound
		}

		batches, err := s.store.ListByVariantID(ctx, variantID)
		if err != nil {
			return time.Time{}, apierror.ErrInternal
		}

		openBatches := make([]PreorderBatch, 0)
		batchIDs := make([]string, 0)
		for _, batch := range batches {
			if batch.Status == StatusActive || batch.Status == StatusQueued {
				openBatches = append(openBatches, batch)
				batchIDs = append(batchIDs, batch.ID)
			}
		}
		if len(openBatches) == 0 {
			continue
		}

		lockedByBatch, err := s.store.GetActiveLockedQtyByBatchIDs(ctx, batchIDs, userID, sessionID)
		if err != nil {
			return time.Time{}, err
		}

		preview, err := s.store.AllocateToBatch(ctx, AllocateInput{
			VariantID:          variantID,
			ShopifyVariantID:   req.ShopifyVariantID,
			ProductTitle:       req.ProductTitle,
			ImageURL:           req.ImageURL,
			Quantity:           req.Quantity,
			Commit:             false,
			LockedQtyByBatchID: lockedByBatch,
		})
		if err != nil {
			return time.Time{}, apierror.ErrInternal
		}
		if preview == nil {
			continue
		}

		for _, line := range preview.Allocations {
			if line.Quantity < 1 {
				continue
			}
			available := 0
			for _, batch := range openBatches {
				if batch.ID != line.BatchID {
					continue
				}
				available = batch.QtyRemaining() - lockedByBatch[batch.ID]
				break
			}
			if available < line.Quantity {
				return time.Time{}, apierror.New(422, "batch_out_of_stock", "Not enough pre-order batch quota available for variant: "+req.ShopifyVariantID)
			}
			newLocks = append(newLocks, BatchLock{
				BatchID:           line.BatchID,
				ShopifyVariantID:  req.ShopifyVariantID,
				Quantity:          line.Quantity,
				SessionID:         sessionID,
				UserID:            userID,
				CheckoutReference: &checkoutRef,
				ExpiresAt:         expiresAt,
			})
			lockedByBatch[line.BatchID] += line.Quantity
		}
	}

	if err := s.store.CreateBatchLocks(ctx, newLocks); err != nil {
		return time.Time{}, err
	}
	return expiresAt, nil
}

func (s *batchLockService) ReleaseBatchLocks(ctx context.Context, userID, sessionID *string) error {
	return s.store.DeleteBatchLocksBySession(ctx, userID, sessionID)
}

func (s *batchLockService) ReleaseBatchLocksByCheckoutReference(ctx context.Context, checkoutReference string) error {
	return s.store.DeleteBatchLocksByCheckoutReference(ctx, checkoutReference, nil, nil)
}

func (s *batchLockService) ReleaseBatchLocksForIdentity(
	ctx context.Context,
	userID, sessionID *string,
	checkoutReference *string,
) error {
	if checkoutReference != nil && *checkoutReference != "" {
		return s.store.DeleteBatchLocksByCheckoutReference(ctx, *checkoutReference, userID, sessionID)
	}
	return s.store.DeleteBatchLocksBySession(ctx, userID, sessionID)
}

func (s *batchLockService) CleanExpiredBatchLocks(ctx context.Context) error {
	return s.store.DeleteExpiredBatchLocks(ctx)
}
