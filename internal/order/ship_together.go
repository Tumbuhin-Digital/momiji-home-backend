package order

import (
	"context"
	"net/http"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

// IsShipTogetherHoldActive reports whether ship-ready Shopify fulfillment must stay blocked.
func IsShipTogetherHoldActive(o *Order) bool {
	if o == nil || !o.ShipTogether {
		return false
	}
	return !isShipTogetherHoldReleased(o)
}

// isShipTogetherHoldReleased is true when every pre-order fulfillment group has second payment paid.
// Unbatched pre-order slices without a batch shipment must also be paid before release.
func isShipTogetherHoldReleased(o *Order) bool {
	if o == nil || !o.ShipTogether {
		return true
	}
	hasPreOrder := false
	for _, item := range o.Items {
		if item.Type != "pre_order" {
			continue
		}
		hasPreOrder = true
		break
	}
	if !hasPreOrder {
		return true
	}
	// Fallback when groups are not loaded: require all pre-order lines at payment_received or beyond.
	for _, item := range o.Items {
		if item.Type != "pre_order" {
			continue
		}
		if item.FulfillmentStep < preOrderStepShipped && item.ItemStatus != "payment_received" && item.ItemStatus != "shipped" && item.ItemStatus != "delivered" {
			return false
		}
	}
	return true
}

// IsShipTogetherHoldReleasedForGroups uses fulfillment group second-payment status when available.
func IsShipTogetherHoldReleasedForGroups(o *Order, groups []FulfillmentGroupDTO) bool {
	if o == nil || !o.ShipTogether {
		return true
	}
	hasPreOrderGroup := false
	for _, g := range groups {
		if g.Kind != FulfillmentGroupPreorderBatch && g.Kind != FulfillmentGroupPreorderUnbatched {
			continue
		}
		hasPreOrderGroup = true
		if g.SecondPaymentStatus != "paid" {
			return false
		}
	}
	if !hasPreOrderGroup {
		return isShipTogetherHoldReleased(o)
	}
	return true
}

func shipTogetherAcceptBlockedError(o *Order) error {
	batchLabel := "pre-order batch"
	if o != nil && o.HoldUntilBatch != nil && *o.HoldUntilBatch != "" {
		batchLabel = *o.HoldUntilBatch
	}
	return apierror.New(http.StatusConflict, "ship_together_hold_active",
		"Order is held for combined shipment until "+batchLabel+" second payment is complete")
}

func (s *service) appendShipReadyForCombinedShipping(ctx context.Context, o *Order, batchID *string, groupItems []OrderItem) ([]OrderItem, error) {
	if o == nil || !o.ShipTogether {
		return groupItems, nil
	}
	include, err := s.shouldIncludeShipReadyInBatchGroup(ctx, o, batchID)
	if err != nil || !include {
		return groupItems, err
	}
	out := make([]OrderItem, len(groupItems), len(groupItems)+len(o.Items))
	copy(out, groupItems)
	for _, it := range o.Items {
		if it.Type == "ship_ready" {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *service) shouldIncludeShipReadyInBatchGroup(ctx context.Context, o *Order, batchID *string) (bool, error) {
	if o == nil || !o.ShipTogether {
		return false, nil
	}
	if s.batchService == nil {
		return batchID == nil, nil
	}

	preLineIDs := make([]string, 0)
	for _, it := range o.Items {
		if it.Type == "pre_order" {
			preLineIDs = append(preLineIDs, it.ID)
		}
	}
	if len(preLineIDs) == 0 {
		return batchID == nil, nil
	}

	allocs, err := s.batchService.GetCommittedAllocationsByOrderLineItemIDs(ctx, preLineIDs)
	if err != nil {
		return false, err
	}

	batchIDSet := make(map[string]struct{})
	allocatedQty := make(map[string]int)
	for _, a := range allocs {
		if a.OrderLineItemID != nil {
			allocatedQty[*a.OrderLineItemID] += a.Quantity
		}
		if a.BatchID != "" {
			batchIDSet[a.BatchID] = struct{}{}
		}
	}

	hasUnbatched := false
	for _, it := range o.Items {
		if it.Type != "pre_order" {
			continue
		}
		if it.Quantity-allocatedQty[it.ID] > 0 {
			hasUnbatched = true
			break
		}
	}

	wantBatch := ""
	if batchID != nil {
		wantBatch = *batchID
	}

	if wantBatch == "" {
		return hasUnbatched && len(batchIDSet) == 0, nil
	}
	if len(batchIDSet) == 0 {
		return false, nil
	}

	batchIDs := make([]string, 0, len(batchIDSet))
	for id := range batchIDSet {
		batchIDs = append(batchIDs, id)
	}
	batches, err := s.batchService.GetBatchesByIDs(ctx, batchIDs)
	if err != nil {
		return false, err
	}
	if len(batches) == 0 {
		return false, nil
	}

	latest := batches[0]
	for _, b := range batches[1:] {
		if b.Sequence > latest.Sequence {
			latest = b
		}
	}
	return latest.ID == wantBatch, nil
}

func (s *service) markShipTogetherCombinedGroups(ctx context.Context, o *Order, groups []FulfillmentGroupDTO) error {
	if o == nil || !o.ShipTogether || len(groups) == 0 {
		return nil
	}
	for i := range groups {
		var batchID *string
		if groups[i].BatchID != nil {
			batchID = groups[i].BatchID
		}
		include, err := s.shouldIncludeShipReadyInBatchGroup(ctx, o, batchID)
		if err != nil {
			return err
		}
		groups[i].IncludesShipReady = include
	}
	return nil
}
