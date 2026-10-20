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

	// The "(Deposit 50%)" suffix is stripped back off by the settlement invoice and the
	// order-confirmed page, so keep that exact wording. The line is identified as a
	// pre-order by its "type" attribute, never by its title.
	draftLine := shopify.DraftOrderLineItem{
		Title:             fmt.Sprintf("%s (Deposit 50%%)", item.Title),
		SKU:               item.SKU,
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

// PreOrderShippingDepositTitle labels the upfront 50% shipping charge on the
// checkout draft order. The remaining half is billed with the settlement invoice.
const PreOrderShippingDepositTitle = "Shipping (Pre-Order) - 50%"

// PreOrderShippingPrepaidAttribute carries the upfront shipping amount on the draft
// order so the paid-order webhook can record what the settlement invoice must not re-bill.
const PreOrderShippingPrepaidAttribute = "preorder_shipping_prepaid"

func buildPreOrderShippingDepositLine(amount float64) shopify.DraftOrderLineItem {
	requiresShipping := false
	return shopify.DraftOrderLineItem{
		Title:             PreOrderShippingDepositTitle,
		Quantity:          1,
		OriginalUnitPrice: fmt.Sprintf("%.2f", amount),
		RequiresShipping:  &requiresShipping,
		CustomAttributes: []shopify.AttributeInput{
			{Key: "charge_type", Value: "pre_order_shipping_deposit"},
		},
	}
}

// preorderDepositTags tags a checkout draft that collects a pre-order deposit, so the
// resulting Shopify order can be told apart from the later order for the balance.
func preorderDepositTags(preOrder []cart.CartItem) []string {
	if len(preOrder) == 0 {
		return nil
	}
	return []string{shopify.TagPreorderDeposit}
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
