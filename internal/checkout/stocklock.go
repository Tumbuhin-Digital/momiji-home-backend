package checkout

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

const stockLockTTL = 15 * time.Minute

type LockRequest struct {
	ShopifyVariantID string
	Quantity         int
}

type StockLockService interface {
	AcquireLocks(ctx context.Context, userID, sessionID *string, checkoutReference string, requests []LockRequest) (time.Time, error)
	ReleaseLocks(ctx context.Context, userID, sessionID *string) error
	ReleaseLocksByCheckoutReference(ctx context.Context, checkoutReference string) error
	ReleaseLocksForIdentity(ctx context.Context, userID, sessionID *string, checkoutReference *string) error
	CleanExpiredLocks(ctx context.Context) error
}

type stockLockService struct {
	store          StockLockStore
	productService product.ProductService
	shopifyCli     shopify.Client
}

func NewStockLockService(store StockLockStore, productService product.ProductService, shopifyCli shopify.Client) StockLockService {
	return &stockLockService{store: store, productService: productService, shopifyCli: shopifyCli}
}

func (s *stockLockService) AcquireLocks(
	ctx context.Context,
	userID, sessionID *string,
	checkoutReference string,
	requests []LockRequest,
) (time.Time, error) {
	if len(requests) == 0 {
		return time.Time{}, nil
	}

	if err := s.store.DeleteLocksBySession(ctx, userID, sessionID); err != nil {
		return time.Time{}, err
	}

	expiresAt := time.Now().Add(stockLockTTL)
	checkoutRef := checkoutReference

	var variantIDs []string
	for _, req := range requests {
		variantIDs = append(variantIDs, req.ShopifyVariantID)
	}

	realTimeInv, err := s.shopifyCli.GetVariantsInventory(ctx, variantIDs)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to check real-time shopify inventory: %w", err)
	}

	var newLocks []StockLock
	for _, req := range requests {
		inventoryQty, ok := realTimeInv[req.ShopifyVariantID]
		if !ok {
			return time.Time{}, apierror.New(404, "not_found", "Variant not found in Shopify: "+req.ShopifyVariantID)
		}

		lockedQty, err := s.store.GetActiveLocksForVariant(ctx, req.ShopifyVariantID)
		if err != nil {
			return time.Time{}, err
		}

		available := inventoryQty - lockedQty
		if available < req.Quantity {
			return time.Time{}, apierror.New(422, "out_of_stock", "Not enough inventory available for variant: "+req.ShopifyVariantID)
		}

		newLocks = append(newLocks, StockLock{
			ShopifyVariantID:  req.ShopifyVariantID,
			Quantity:          req.Quantity,
			SessionID:         sessionID,
			UserID:            userID,
			CheckoutReference: &checkoutRef,
			ExpiresAt:         expiresAt,
		})
	}

	if err := s.store.CreateLocks(ctx, newLocks); err != nil {
		return time.Time{}, err
	}

	return expiresAt, nil
}

func (s *stockLockService) ReleaseLocks(ctx context.Context, userID, sessionID *string) error {
	return s.store.DeleteLocksBySession(ctx, userID, sessionID)
}

func (s *stockLockService) ReleaseLocksByCheckoutReference(ctx context.Context, checkoutReference string) error {
	return s.store.DeleteLocksByCheckoutReference(ctx, checkoutReference, nil, nil)
}

func (s *stockLockService) ReleaseLocksForIdentity(
	ctx context.Context,
	userID, sessionID *string,
	checkoutReference *string,
) error {
	if checkoutReference != nil && *checkoutReference != "" {
		return s.store.DeleteLocksByCheckoutReference(ctx, *checkoutReference, userID, sessionID)
	}
	return s.store.DeleteLocksBySession(ctx, userID, sessionID)
}

func (s *stockLockService) CleanExpiredLocks(ctx context.Context) error {
	now := time.Now()

	// Cancel Shopify drafts before releasing locks so invoices cannot be paid after TTL.
	sessions, err := s.store.ListExpiredCheckoutSessions(ctx, now)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.ShopifyDraftOrderID == "" {
			continue
		}
		if err := s.shopifyCli.DeleteDraftOrder(ctx, session.ShopifyDraftOrderID); err != nil {
			slog.WarnContext(ctx, "Failed to delete expired checkout draft order",
				slog.String("checkout_reference", session.CheckoutReference),
				slog.String("shopify_draft_order_id", session.ShopifyDraftOrderID),
				slog.Any("error", err))
		}
	}

	if err := s.store.DeleteExpiredCheckoutSessions(ctx, now); err != nil {
		return err
	}
	return s.store.DeleteExpiredLocks(ctx)
}
