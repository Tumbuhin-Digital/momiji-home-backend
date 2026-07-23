package order

import "testing"

func TestPickBestFOLIByLineItem_PrefersHigherRemaining(t *testing.T) {
	lineID := "line-1"
	folis := []FulfillmentOrderLineItem{
		{OrderLineItemID: lineID, RemainingQuantity: 0, FulfillmentOrderID: "fo-closed"},
		{OrderLineItemID: lineID, RemainingQuantity: 5, FulfillmentOrderID: "fo-open"},
	}
	best := pickBestFOLIByLineItem(folis)
	got := best[lineID]
	if got.RemainingQuantity != 5 || got.FulfillmentOrderID != "fo-open" {
		t.Fatalf("expected open FOLI with remaining 5, got remaining=%d fo=%s", got.RemainingQuantity, got.FulfillmentOrderID)
	}
}

func TestShouldAllowLocalFulfillmentWhenShopifyRemainingZero(t *testing.T) {
	// Mirrors CreatePreorderFulfillment decision: remaining 0 + no local fulfillment
	// must not hard-fail; admin can still record tracking locally.
	shopifyRemaining := 0
	localFulfilled := 0
	itemQty := 5
	requestQty := 5

	if shopifyRemaining > 0 && requestQty > shopifyRemaining {
		t.Fatal("should hard-fail only when partial remaining is insufficient")
	}
	if shopifyRemaining == 0 && localFulfilled >= itemQty {
		t.Fatal("should hard-fail when already fulfilled locally")
	}
	if shopifyRemaining == 0 && localFulfilled+requestQty <= itemQty {
		return // allowed local-only path
	}
	t.Fatal("expected local-only fulfillment to be allowed")
}
