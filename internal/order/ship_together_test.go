package order

import (
	"testing"
)

func TestIsShipTogetherHoldActive(t *testing.T) {
	t.Parallel()

	batch := "September Batch"
	o := &Order{
		ShipTogether: true,
		HoldUntilBatch: &batch,
		Items: []OrderItem{
			{Type: "ship_ready", ItemStatus: "paid", FulfillmentStep: 1},
			{Type: "pre_order", ItemStatus: "pending_deposit", FulfillmentStep: 1},
		},
	}
	if !IsShipTogetherHoldActive(o) {
		t.Fatal("expected hold to be active while pre-order unpaid")
	}

	o.Items[1].ItemStatus = "payment_received"
	o.Items[1].FulfillmentStep = preOrderStepShipped
	if IsShipTogetherHoldActive(o) {
		t.Fatal("expected hold to release after pre-order payment_received")
	}
}

func TestIsShipTogetherHoldReleasedForGroups(t *testing.T) {
	t.Parallel()

	o := &Order{ShipTogether: true}
	groups := []FulfillmentGroupDTO{
		{Kind: FulfillmentGroupPreorderBatch, SecondPaymentStatus: "paid"},
		{Kind: FulfillmentGroupPreorderBatch, SecondPaymentStatus: "invoiced"},
	}
	if IsShipTogetherHoldReleasedForGroups(o, groups) {
		t.Fatal("expected hold until all preorder groups paid")
	}

	groups[1].SecondPaymentStatus = "paid"
	if !IsShipTogetherHoldReleasedForGroups(o, groups) {
		t.Fatal("expected hold released when all groups paid")
	}
}

func TestShipTogetherAcceptBlockedError(t *testing.T) {
	t.Parallel()

	batch := "September Batch"
	err := shipTogetherAcceptBlockedError(&Order{HoldUntilBatch: &batch})
	if err == nil {
		t.Fatal("expected error")
	}
}
