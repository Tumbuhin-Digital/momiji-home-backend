package order

import (
	"strings"
	"testing"
)

func TestBuildPreorderShipmentFromPacking_PropagatesNestedError(t *testing.T) {
	preItems := []OrderItem{
		{ID: "line-1", ShopifyVariantID: "var-2", Quantity: 1},
	}
	dims := map[string]VariantDimensions{
		"var-2": {ShopifyVariantID: "var-2", WeightKg: 2},
	}
	packing := []PackingItemDTO{{LineItemID: "line-1", BoxCount: 0, IsNested: true}}

	shipment, dbPacking, err := buildPreorderShipmentFromPacking("order-webhook-1", packing, preItems, dims, nil, "east")
	if err == nil {
		t.Fatal("expected packing error to propagate")
	}
	if shipment != nil || dbPacking != nil {
		t.Fatal("expected nil shipment and packing on error — no partial rate/shipment data")
	}
	if !strings.Contains(err.Error(), "order-webhook-1") {
		t.Fatalf("expected order_id in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "line-1") {
		t.Fatalf("expected nested line_item_id in error, got: %v", err)
	}
}
