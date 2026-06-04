package checkout

import (
	"context"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type LockRequest struct {
	ShopifyVariantID string
	Quantity         int
}

type StockLockService interface {
	AcquireLocks(ctx context.Context, userID, sessionID *string, requests []LockRequest) error
	ReleaseLocks(ctx context.Context, userID, sessionID *string) error
	CleanExpiredLocks(ctx context.Context) error
}

type stockLockService struct {
	store          StockLockStore
	productService product.ProductService
}

func NewStockLockService(store StockLockStore, productService product.ProductService) StockLockService {
	return &stockLockService{store: store, productService: productService}
}

func (s *stockLockService) AcquireLocks(ctx context.Context, userID, sessionID *string, requests []LockRequest) error {
	var newLocks []StockLock
	now := time.Now()
	expiresAt := now.Add(15 * time.Minute)

	for _, req := range requests {
		// Get product inventory
		variant, err := s.productService.GetVariantByID(ctx, req.ShopifyVariantID)
		if err != nil {
			return err
		}

		// Get active locks
		lockedQty, err := s.store.GetActiveLocksForVariant(ctx, req.ShopifyVariantID)
		if err != nil {
			return err
		}

		available := variant.InventoryQuantity - lockedQty
		if available < req.Quantity {
			return apierror.New(422, "out_of_stock", "Not enough inventory available for variant: " + req.ShopifyVariantID)
		}

		newLocks = append(newLocks, StockLock{
			ShopifyVariantID: req.ShopifyVariantID,
			Quantity:         req.Quantity,
			SessionID:        sessionID,
			UserID:           userID,
			ExpiresAt:        expiresAt,
		})
	}

	return s.store.CreateLocks(ctx, newLocks)
}

func (s *stockLockService) ReleaseLocks(ctx context.Context, userID, sessionID *string) error {
	return s.store.DeleteLocksBySession(ctx, userID, sessionID)
}

func (s *stockLockService) CleanExpiredLocks(ctx context.Context) error {
	return s.store.DeleteExpiredLocks(ctx)
}
