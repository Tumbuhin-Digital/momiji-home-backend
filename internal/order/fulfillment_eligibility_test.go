package order

import (
	"testing"
	"time"
)

func TestIsPreorderLineReadyForFulfillment(t *testing.T) {
	tests := []struct {
		name       string
		step       int
		paidQty    int
		requestQty int
		want       bool
	}{
		{name: "step 4 ready", step: 4, paidQty: 0, requestQty: 5, want: true},
		{name: "step 5 ready", step: 5, paidQty: 0, requestQty: 1, want: true},
		{name: "paid shipment covers qty", step: 3, paidQty: 5, requestQty: 5, want: true},
		{name: "paid shipment covers more", step: 2, paidQty: 5, requestQty: 3, want: true},
		{name: "unpaid sibling batch blocks", step: 3, paidQty: 0, requestQty: 4, want: false},
		{name: "partial paid insufficient", step: 3, paidQty: 2, requestQty: 5, want: false},
		{name: "step 3 without payment", step: 3, paidQty: 0, requestQty: 1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPreorderLineReadyForFulfillment(tt.step, tt.paidQty, tt.requestQty)
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPaidQtyByLineFromShipments_IgnoresUnpaidSiblingBatches(t *testing.T) {
	now := time.Now()
	paid := now
	shipments := []PreorderShipment{
		{
			ID:            "ship-dec",
			InvoicePaidAt: &paid,
			PackingItems: []PreorderPackingItem{
				{OrderLineItemID: "line-dec", Quantity: 5},
			},
		},
		{
			ID:            "ship-oct",
			InvoiceSentAt: &now,
			// unpaid — must not block counting paid December qty
			PackingItems: []PreorderPackingItem{
				{OrderLineItemID: "line-oct", Quantity: 4},
			},
		},
		{
			ID: "ship-unbatched",
			PackingItems: []PreorderPackingItem{
				{OrderLineItemID: "line-unbatched", Quantity: 1},
			},
		},
	}

	got := paidQtyByLineFromShipments(shipments)
	if got["line-dec"] != 5 {
		t.Fatalf("expected paid december qty 5, got %d", got["line-dec"])
	}
	if _, ok := got["line-oct"]; ok {
		t.Fatalf("unpaid oktober line should not appear in paid map")
	}
	if _, ok := got["line-unbatched"]; ok {
		t.Fatalf("unpaid unbatched line should not appear in paid map")
	}
}
