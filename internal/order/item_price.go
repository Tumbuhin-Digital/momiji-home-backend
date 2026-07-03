package order

import "fmt"

// formatItemUnitPrice returns the per-unit price shown in order APIs.
// Ship-ready lines use the amount actually charged (wholesale paid), not catalog RPP from Shopify.
func formatItemUnitPrice(it OrderItem) *string {
	if it.Type == "ship_ready" && it.AmountCharged != nil && it.Quantity > 0 {
		perUnit := *it.AmountCharged / float64(it.Quantity)
		val := fmt.Sprintf("%.2f", perUnit)
		return &val
	}
	if it.UnitPrice != nil {
		val := fmt.Sprintf("%.2f", *it.UnitPrice)
		return &val
	}
	return nil
}
