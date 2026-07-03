package webhook

// isPreorderShopifyLineItem reports whether a paid-order line is a preorder deposit.
// Classification uses line item properties only; variant fulfillment type is not used
// because the same variant can appear as ship-ready (full price) and preorder (deposit) in mixed carts.
func isPreorderShopifyLineItem(item ShopifyOrderLineItem) bool {
	for _, prop := range item.Properties {
		if prop.Name == "type" {
			if valStr, ok := prop.Value.(string); ok && valStr == "preorder_dp" {
				return true
			}
		}
	}
	return false
}
