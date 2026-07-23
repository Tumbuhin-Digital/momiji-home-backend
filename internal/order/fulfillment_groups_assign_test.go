package order

import "testing"

func TestAssignFulfillmentsToPreorderGroups_SharedLineDoesNotLeak(t *testing.T) {
	lineID := "line-shared"
	decID := "batch-dec"
	octID := "batch-oct"

	groups := []FulfillmentGroupDTO{
		{
			Key:      "batch:" + decID,
			Kind:     FulfillmentGroupPreorderBatch,
			BatchID:  &decID,
			BatchName: "Batch - December",
			LineSlices: []OrderLineSliceDTO{
				{LineItemID: lineID, Quantity: 5, RemainingQuantity: 5},
			},
		},
		{
			Key:      "batch:" + octID,
			Kind:     FulfillmentGroupPreorderBatch,
			BatchID:  &octID,
			BatchName: "Batch Oktober",
			LineSlices: []OrderLineSliceDTO{
				{LineItemID: lineID, Quantity: 4, RemainingQuantity: 4},
			},
			SecondPaymentStatus: "ready",
		},
		{
			Key:  "preorder_unbatched",
			Kind: FulfillmentGroupPreorderUnbatched,
			LineSlices: []OrderLineSliceDTO{
				{LineItemID: lineID, Quantity: 1, RemainingQuantity: 1},
			},
		},
	}

	tracking := "adadasda"
	all := []FulfillmentDTO{
		{
			ID:             "f1",
			DisplayID:      "#ORD-1247-F1",
			SequenceNumber: 1,
			TrackingNumber: tracking,
			Status:         "fulfilled",
			LineItems: []FulfillmentLineItemDTO{
				{LineItemID: lineID, Title: "Shelf", Quantity: 5, UnitPrice: "299.00"},
			},
		},
	}

	assignFulfillmentsToPreorderGroups(groups, all)
	for i := range groups {
		scrubGroupSliceFulfillmentState(&groups[i])
	}

	if len(groups[0].Fulfillments) != 1 {
		t.Fatalf("december should own the fulfillment, got %d", len(groups[0].Fulfillments))
	}
	if len(groups[1].Fulfillments) != 0 {
		t.Fatalf("oktober must not inherit december fulfillment, got %d", len(groups[1].Fulfillments))
	}
	if len(groups[2].Fulfillments) != 0 {
		t.Fatalf("unbatched must not inherit december fulfillment, got %d", len(groups[2].Fulfillments))
	}

	if groups[0].LineSlices[0].RemainingQuantity != 0 {
		t.Fatalf("december remaining want 0, got %d", groups[0].LineSlices[0].RemainingQuantity)
	}
	if groups[1].LineSlices[0].TrackingNumber != nil {
		t.Fatal("oktober slice must not keep december tracking")
	}
	if groups[1].LineSlices[0].RemainingQuantity != 4 {
		t.Fatalf("oktober remaining want 4, got %d", groups[1].LineSlices[0].RemainingQuantity)
	}
}

func TestScrubGroupSliceFulfillmentState_ReadyGroupClearsSiblingWaitingPayment(t *testing.T) {
	group := FulfillmentGroupDTO{
		Kind:                FulfillmentGroupPreorderUnbatched,
		SecondPaymentStatus: "ready",
		LineSlices: []OrderLineSliceDTO{
			{
				LineItemID:      "line-shared",
				Quantity:        1,
				ItemStatus:      "waiting_payment", // inherited from sibling batch invoice
				FulfillmentStep: 3,
			},
		},
	}

	scrubGroupSliceFulfillmentState(&group)

	if group.LineSlices[0].ItemStatus != "paid" {
		t.Fatalf("ready unbatched group want item status paid, got %q", group.LineSlices[0].ItemStatus)
	}
	if group.LineSlices[0].FulfillmentStep != preOrderStepShippingConfigured {
		t.Fatalf("ready unbatched group want step 2, got %d", group.LineSlices[0].FulfillmentStep)
	}
}

func TestScrubGroupSliceFulfillmentState_InvoicedGroupKeepsWaitingPayment(t *testing.T) {
	group := FulfillmentGroupDTO{
		Kind:                FulfillmentGroupPreorderBatch,
		SecondPaymentStatus: "invoiced",
		LineSlices: []OrderLineSliceDTO{
			{
				LineItemID:      "line-shared",
				Quantity:        4,
				ItemStatus:      "waiting_payment",
				FulfillmentStep: 3,
			},
		},
	}

	scrubGroupSliceFulfillmentState(&group)

	if group.LineSlices[0].ItemStatus != "waiting_payment" {
		t.Fatalf("invoiced group want waiting_payment, got %q", group.LineSlices[0].ItemStatus)
	}
}

func TestAssignFulfillmentsToPreorderGroups_PrefersBatchID(t *testing.T) {
	lineID := "line-shared"
	decID := "batch-dec"
	octID := "batch-oct"

	groups := []FulfillmentGroupDTO{
		{
			Key:     "batch:" + decID,
			Kind:    FulfillmentGroupPreorderBatch,
			BatchID: &decID,
			LineSlices: []OrderLineSliceDTO{
				{LineItemID: lineID, Quantity: 5},
			},
		},
		{
			Key:     "batch:" + octID,
			Kind:    FulfillmentGroupPreorderBatch,
			BatchID: &octID,
			LineSlices: []OrderLineSliceDTO{
				{LineItemID: lineID, Quantity: 5},
			},
		},
	}

	all := []FulfillmentDTO{
		{
			ID:             "f-oct",
			SequenceNumber: 1,
			BatchID:        &octID,
			Status:         "fulfilled",
			LineItems: []FulfillmentLineItemDTO{
				{LineItemID: lineID, Quantity: 5},
			},
		},
	}

	assignFulfillmentsToPreorderGroups(groups, all)
	if len(groups[0].Fulfillments) != 0 {
		t.Fatal("december should not receive oktober-scoped fulfillment")
	}
	if len(groups[1].Fulfillments) != 1 {
		t.Fatalf("oktober should own batch-scoped fulfillment, got %d", len(groups[1].Fulfillments))
	}
}
