package checkout

import (
	"fmt"
	"strconv"

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

func buildPreOrderDraftLine(item cart.CartItem) shopify.DraftOrderLineItem {
	unitPrice, _ := strconv.ParseFloat(item.UnitPrice, 64)
	deposit := unitPrice * 0.5
	requiresShipping := false

	draftLine := shopify.DraftOrderLineItem{
		Title:             fmt.Sprintf("[PREORDER] %s (Deposit 50%%)", item.Title),
		Quantity:          item.Quantity,
		OriginalUnitPrice: fmt.Sprintf("%.2f", deposit),
		RequiresShipping:  &requiresShipping,
		CustomAttributes: []shopify.AttributeInput{
			{Key: "type", Value: "preorder_dp"},
			{Key: "full_price", Value: fmt.Sprintf("%.2f", unitPrice)},
			{Key: "variant_ref", Value: item.VariantID},
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

func buildDraftLinesFromSegments(shipReady, preOrder []cart.CartItem) []shopify.DraftOrderLineItem {
	var draftLines []shopify.DraftOrderLineItem
	for _, item := range shipReady {
		draftLines = append(draftLines, buildShipReadyDraftLine(item))
	}
	for _, item := range preOrder {
		draftLines = append(draftLines, buildPreOrderDraftLine(item))
	}
	return draftLines
}
