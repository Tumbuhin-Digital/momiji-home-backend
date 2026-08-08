package checkout

import (
	"context"
	"errors"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

func isOutOfStockError(err error) bool {
	var apiErr *apierror.AppError
	if errors.As(err, &apiErr) {
		return apiErr.Code == "out_of_stock"
	}
	return false
}

func shipReadyInventoryDepletedError(details *cart.ShipReadyInventoryDepletionEvent) error {
	return apierror.NewWithDetails(
		409,
		"ship_ready_inventory_depleted",
		"Ship ready inventory has changed; cart was updated to move unavailable quantities to pre-order",
		details,
	)
}

// reconcileShipReadyInventory fetches live Shopify inventory for ship-ready cart lines,
// syncs local DB, and re-splits cart. Returns the first depletion event when cart changed.
func (s *service) reconcileShipReadyInventory(
	ctx context.Context,
	userID, sessionID *string,
	shipReady []cart.CartItem,
) (*cart.ShipReadyInventoryDepletionEvent, error) {
	if len(shipReady) == 0 || s.shopifyCli == nil {
		return nil, nil
	}

	variantIDs := make([]string, 0, len(shipReady))
	seen := make(map[string]struct{}, len(shipReady))
	for _, item := range shipReady {
		if item.VariantID == "" {
			continue
		}
		if _, ok := seen[item.VariantID]; ok {
			continue
		}
		seen[item.VariantID] = struct{}{}
		variantIDs = append(variantIDs, item.VariantID)
	}
	if len(variantIDs) == 0 {
		return nil, nil
	}

	liveInv, err := s.shopifyCli.GetVariantsInventory(ctx, variantIDs)
	if err != nil {
		return nil, err
	}

	if s.productService != nil {
		if err := s.productService.SyncInventoryQuantities(ctx, liveInv); err != nil {
			return nil, err
		}
	}

	result, err := s.cartService.ReconcileShipReadyAgainstInventory(ctx, userID, sessionID, liveInv)
	if err != nil {
		return nil, err
	}
	if result == nil || !result.Changed || len(result.Variants) == 0 {
		return nil, nil
	}
	first := result.Variants[0]
	return &first, nil
}
