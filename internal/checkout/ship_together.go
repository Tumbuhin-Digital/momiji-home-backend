package checkout

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

func validateShipTogether(cartRes *cart.CartResponse, shipTogether bool) error {
	if cartRes == nil {
		return nil
	}
	return validateShipTogetherSegments(cartRes.ShipReady, cartRes.PreOrder, shipTogether)
}

func validateShipTogetherSegments(shipReady, preOrder []cart.CartItem, shipTogether bool) error {
	if !shipTogether {
		return nil
	}
	if len(shipReady) == 0 || len(preOrder) == 0 {
		return apierror.New(400, "bad_request", "ship_together requires a mixed cart with ship ready and pre-order items")
	}
	return nil
}

// applyShipTogetherSegments reclassifies ship-ready items as pre-order when ship_together is on.
// Cart DB is unchanged; this only affects checkout/manual draft construction.
func applyShipTogetherSegments(shipReady, preOrder []cart.CartItem, shipTogether bool) (effectiveShipReady, effectivePreOrder []cart.CartItem) {
	if !shipTogether {
		return shipReady, preOrder
	}
	return nil, mergeCartItemsByVariant(append(append([]cart.CartItem{}, shipReady...), preOrder...))
}

func mergeCartItemsByVariant(items []cart.CartItem) []cart.CartItem {
	if len(items) == 0 {
		return nil
	}
	order := make([]string, 0, len(items))
	byVariant := make(map[string]cart.CartItem, len(items))
	for _, item := range items {
		if existing, ok := byVariant[item.VariantID]; ok {
			existing.Quantity += item.Quantity
			unitPrice, _ := parseMoney(item.UnitPrice)
			if unitPrice <= 0 {
				unitPrice, _ = parseMoney(existing.UnitPrice)
			}
			existing.Subtotal = fmt.Sprintf("%.2f", unitPrice*float64(existing.Quantity))
			byVariant[item.VariantID] = existing
			continue
		}
		order = append(order, item.VariantID)
		byVariant[item.VariantID] = item
	}
	out := make([]cart.CartItem, 0, len(order))
	for _, id := range order {
		out = append(out, byVariant[id])
	}
	return out
}

func parseMoney(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

func collectCheckoutBatchNames(ctx context.Context, batchService BatchPreviewer, preOrder []cart.CartItem, userID, sessionID *string) (string, error) {
	if batchService == nil || len(preOrder) == 0 {
		return "", nil
	}
	nameSet := make(map[string]struct{})
	for _, item := range preOrder {
		preview, err := batchService.PreviewAllocation(ctx, item.VariantID, item.Quantity, userID, sessionID)
		if err != nil {
			return "", err
		}
		if preview == nil {
			continue
		}
		for _, alloc := range preview.Allocations {
			if alloc.BatchName != "" {
				nameSet[alloc.BatchName] = struct{}{}
			}
		}
	}
	if len(nameSet) == 0 {
		return "", nil
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", "), nil
}

func formatShipTogetherHoldNote(batchNames string) string {
	if batchNames == "" {
		return "DO NOT FULFIL ANY ITEM IN THIS ORDER UNTIL PRE-ORDER BATCH IS READY"
	}
	return fmt.Sprintf("DO NOT FULFIL ANY ITEM IN THIS ORDER UNTIL %s", batchNames)
}
