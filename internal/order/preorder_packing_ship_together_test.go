package order

import "testing"

func TestMergeDefaultPackingForMissingItems(t *testing.T) {
	t.Parallel()

	group := []OrderItem{
		{ID: "pre-1", Quantity: 3, Type: "pre_order"},
		{ID: "sr-1", Quantity: 2, Type: "ship_ready"},
	}
	packing := []PackingItemDTO{
		{LineItemID: "pre-1", Quantity: 3, BoxCount: 3},
	}

	merged := mergeDefaultPackingForMissingItems(packing, group)
	if len(merged) != 2 {
		t.Fatalf("expected 2 packing rows, got %d", len(merged))
	}
	var foundSR bool
	for _, p := range merged {
		if p.LineItemID == "sr-1" {
			foundSR = true
			if p.Quantity != 2 || p.BoxCount != 2 {
				t.Fatalf("unexpected ship-ready packing: %+v", p)
			}
		}
	}
	if !foundSR {
		t.Fatal("expected ship-ready packing row to be added")
	}
}
