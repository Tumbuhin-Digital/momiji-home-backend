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

// isPreorderShippingDepositLineItem reports whether a paid-order line is the upfront 50%
// pre-order shipping charge. It carries no variant, so it must be recognised explicitly
// or it would be dropped as an unknown product and left out of the order totals.
func isPreorderShippingDepositLineItem(item ShopifyOrderLineItem) bool {
	for _, prop := range item.Properties {
		if prop.Name == "charge_type" {
			if valStr, ok := prop.Value.(string); ok && valStr == "pre_order_shipping_deposit" {
				return true
			}
		}
	}
	return false
}
