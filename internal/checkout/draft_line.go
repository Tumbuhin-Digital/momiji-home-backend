package checkout

import (
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

func buildShipReadyDraftLine(item cart.CartItem) shopify.DraftOrderLineItem {
	draftLine := shopify.DraftOrderLineItem{
		VariantID: item.VariantID,
		Quantity:  item.Quantity,
		PriceOverride: &shopify.MoneyInput{
			Amount:       item.UnitPrice,
			CurrencyCode: "USD",
		},
	}
	if item.Weight > 0 {
		draftLine.Weight = &shopify.DraftOrderLineItemWeightInput{
			Unit:  "KILOGRAMS",
			Value: item.Weight,
		}
	}
	return draftLine
}
