package webhook

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
)

func TestParsePaidCheckoutMeta_ShipTogether(t *testing.T) {
	meta := parsePaidCheckoutMeta(ShopifyOrderWebhook{
		NoteAttributes: []ShopifyProperty{
			{Name: "ship_together", Value: "true"},
			{Name: "hold_until_batch", Value: "September Batch"},
		},
	})
	if !meta.ShipTogether {
		t.Fatal("expected ship_together true")
	}
	if meta.HoldUntilBatch != "September Batch" {
		t.Fatalf("got hold_until_batch %q", meta.HoldUntilBatch)
	}
}

func TestIsWholesaleMomijiOrder_WithWholesaleSource(t *testing.T) {
	payload := ShopifyOrderWebhook{
		ID: 1,
		NoteAttributes: []ShopifyProperty{
			{Name: "checkout_reference", Value: "uuid-1"},
			{Name: "source", Value: "wholesale"},
		},
	}
	if !isWholesaleMomijiOrder(payload) {
		t.Fatal("expected wholesale order to be accepted")
	}
}

func TestIsWholesaleMomijiOrder_SettlementInvoiceStillWholesale(t *testing.T) {
	payload := ShopifyOrderWebhook{
		ID: 2,
		NoteAttributes: []ShopifyProperty{
			{Name: "source", Value: "wholesale"},
		},
		LineItems: []ShopifyOrderLineItem{
			{
				Title: "Remaining Balance",
				Properties: []ShopifyProperty{
					{Name: "settlement_id", Value: "settlement-uuid"},
				},
			},
		},
	}
	if !isWholesaleMomijiOrder(payload) {
		t.Fatal("expected settlement invoice order with source=wholesale to be accepted")
	}
}

func TestIsWholesaleMomijiOrder_NoNoteAttributes(t *testing.T) {
	payload := ShopifyOrderWebhook{ID: 3}
	if isWholesaleMomijiOrder(payload) {
		t.Fatal("expected order without note attributes to be rejected")
	}
}

func TestIsWholesaleMomijiOrder_WrongSource(t *testing.T) {
	payload := ShopifyOrderWebhook{
		ID: 4,
		NoteAttributes: []ShopifyProperty{
			{Name: "source", Value: "online_store"},
		},
	}
	if isWholesaleMomijiOrder(payload) {
		t.Fatal("expected non-wholesale source to be rejected")
	}
}

type countingOrderStore struct {
	fulfillmentTestStore
	createCalls int
}

func (s *countingOrderStore) CreateOrder(context.Context, *order.Order) error {
	s.createCalls++
	return nil
}

func TestHandleOrderPaid_SkipsNonWholesale(t *testing.T) {
	store := &countingOrderStore{}
	svc := &service{orderStore: store}

	err := svc.HandleOrderPaid(context.Background(), ShopifyOrderWebhook{
		ID:          304,
		OrderNumber: 1001,
		Email:       "retail@example.com",
		Currency:    "USD",
		LineItems: []ShopifyOrderLineItem{
			{VariantID: 1, Title: "Shelf", Quantity: 1, Price: "399.00"},
		},
		Customer: ShopifyCustomer{ID: 9, Email: "retail@example.com"},
	})
	if err != nil {
		t.Fatalf("expected skip to return nil, got %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("expected CreateOrder not to be called, got %d calls", store.createCalls)
	}
}
