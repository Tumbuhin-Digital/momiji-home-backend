package checkout

import (
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

func TestPreorderDepositTags(t *testing.T) {
	if tags := preorderDepositTags(nil); tags != nil {
		t.Fatalf("ship-ready-only checkout must not be tagged, got %v", tags)
	}
	tags := preorderDepositTags([]cart.CartItem{{VariantID: "v1", Quantity: 1}})
	if len(tags) != 1 || tags[0] != shopify.TagPreorderDeposit {
		t.Fatalf("expected [%s], got %v", shopify.TagPreorderDeposit, tags)
	}
}

func TestBuildPreOrderDraftLine_ProductTitleAndSKU(t *testing.T) {
	line := buildPreOrderDraftLine(cart.CartItem{
		VariantID: "gid://shopify/ProductVariant/1",
		Title:     "After the Rain Shelf",
		SKU:       "MU-XJLK-PKQ2",
		Quantity:  3,
		UnitPrice: "350.00",
	})

	if line.Title != "After the Rain Shelf (Deposit 50%)" {
		t.Fatalf("title = %q", line.Title)
	}
	if line.SKU != "MU-XJLK-PKQ2" {
		t.Fatalf("sku = %q", line.SKU)
	}
	// A real variant would make Shopify deduct ship-ready stock for a pre-order.
	if line.VariantID != "" {
		t.Fatalf("deposit line must stay a custom line item, got variant %q", line.VariantID)
	}
	if line.OriginalUnitPrice != "175.00" || line.Quantity != 3 {
		t.Fatalf("unexpected price/qty: %s x %d", line.OriginalUnitPrice, line.Quantity)
	}
}
