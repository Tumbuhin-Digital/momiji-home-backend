package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
)

// stateOnlyEditor answers balance-state lookups; any edit call would panic on the nil
// embedded editor, proving reconciliation never edits an order.
type stateOnlyEditor struct {
	shopify.OrderEditor
	state *shopify.OrderBalanceState
	calls int
}

func (e *stateOnlyEditor) GetOrderBalanceState(context.Context, string) (*shopify.OrderBalanceState, error) {
	e.calls++
	return e.state, nil
}

// reconcileBalanceStore is a deposit order whose balance was billed by an edit.
type reconcileBalanceStore struct {
	cascadeOrderStore
	paid         []string
	balanceLines []order.PreorderBalanceLine
}

func (s *reconcileBalanceStore) MarkPreorderShipmentInvoicePaid(_ context.Context, shipmentID string, paidAt time.Time) error {
	s.paid = append(s.paid, shipmentID)
	for i := range s.shipments {
		if s.shipments[i].ID == shipmentID {
			s.shipments[i].InvoicePaidAt = &paidAt
		}
	}
	return nil
}

func (s *reconcileBalanceStore) ListAwaitingOrderEditBalances(context.Context) ([]order.PreorderShipment, error) {
	var out []order.PreorderShipment
	for _, sh := range s.shipments {
		if sh.AwaitingOrderEditPayment() {
			out = append(out, sh)
		}
	}
	return out, nil
}

func (s *reconcileBalanceStore) GetBalanceLinesByOrderID(context.Context, string) ([]order.PreorderBalanceLine, error) {
	return s.balanceLines, nil
}

func balanceReconcileFixture(outstanding float64) (*reconcileBalanceStore, *cascadePreorderStore, *stateOnlyEditor, *service) {
	shopifyOrderID := "1325"
	method, sent := order.BalanceMethodOrderEdit, time.Now()
	store := &reconcileBalanceStore{cascadeOrderStore: cascadeOrderStore{
		fulfillmentTestStore: fulfillmentTestStore{order: &order.Order{
			ID:             "order-1",
			ShopifyOrderID: &shopifyOrderID,
			Items: []order.OrderItem{
				{ID: "line-1", Type: "pre_order", Quantity: 3, FulfillmentStep: 3, ItemStatus: "waiting_payment"},
			},
		}},
		shipments: []order.PreorderShipment{{
			ID: "ship-1", OrderID: "order-1", BalanceMethod: &method, InvoiceSentAt: &sent,
			PackingItems: []order.PreorderPackingItem{{OrderLineItemID: "line-1", Quantity: 3}},
		}},
	}}
	preorderStore := &cascadePreorderStore{settlements: map[string]*preorder.Settlement{
		"line-1": {ID: "st-1", OrderLineItemID: "line-1", Status: "invoiced"},
	}}
	editor := &stateOnlyEditor{state: &shopify.OrderBalanceState{Outstanding: outstanding, FinancialStatus: "PAID"}}
	svc := &service{orderStore: store, preorderStore: preorderStore, orderEditor: editor}
	return store, preorderStore, editor, svc
}

func TestReconcileOrderEditBalance_PaidDepositOrderSettlesTheGroup(t *testing.T) {
	store, preorderStore, _, svc := balanceReconcileFixture(0)

	if err := svc.ReconcileOrderEditBalance(context.Background(), store.order); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.paid) != 1 || store.paid[0] != "ship-1" {
		t.Fatalf("expected ship-1 marked paid, got %v", store.paid)
	}
	if store.statusByID["line-1"] != "payment_received" || store.stepByID["line-1"] != 4 {
		t.Fatalf("expected the item ready to ship, got %q / %d", store.statusByID["line-1"], store.stepByID["line-1"])
	}
	if len(preorderStore.paidIDs) != 1 || preorderStore.paidIDs[0] != "st-1" {
		t.Fatalf("expected the settlement paid, got %v", preorderStore.paidIDs)
	}
}

func TestReconcileOrderEditBalance_UnpaidBalanceChangesNothing(t *testing.T) {
	store, preorderStore, editor, svc := balanceReconcileFixture(611.56)

	if err := svc.ReconcileOrderEditBalance(context.Background(), store.order); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.calls != 1 || len(store.paid) != 0 || len(preorderStore.paidIDs) != 0 {
		t.Fatalf("an unpaid balance must not settle anything: calls=%d paid=%v", editor.calls, store.paid)
	}
}

func TestReconcileOrderEditBalance_NoAwaitingGroupSkipsShopify(t *testing.T) {
	store, _, editor, svc := balanceReconcileFixture(0)
	store.shipments[0].BalanceMethod = nil // billed by draft invoice

	if err := svc.ReconcileOrderEditBalance(context.Background(), store.order); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.calls != 0 {
		t.Fatal("orders without an edited balance must not query Shopify")
	}
}

func TestReconcilePendingOrderEditBalances_SettlesEachOrderOnce(t *testing.T) {
	store, _, editor, svc := balanceReconcileFixture(0)

	if err := svc.ReconcilePendingOrderEditBalances(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.calls != 1 || len(store.paid) != 1 {
		t.Fatalf("expected one lookup and one payment, got calls=%d paid=%v", editor.calls, store.paid)
	}
}

// The product line an edit added carries the product title, not the stored deposit title,
// so tracking must still reach the item through the stored line link.
func TestHandleFulfillment_TrackingReachesAnEditedBalanceLine(t *testing.T) {
	title := "After the Rain Shelf (Deposit 50%)"
	store := &reconcileBalanceStore{
		cascadeOrderStore: cascadeOrderStore{fulfillmentTestStore: fulfillmentTestStore{order: &order.Order{
			ID:              "order-1",
			FinancialStatus: "paid",
			AggregateStatus: "on_progress",
			Customer:        &customer.Customer{},
			Items: []order.OrderItem{{
				ID: "line-1", Type: "pre_order", ShopifyVariantID: "gid://shopify/ProductVariant/501",
				Title: &title, ItemStatus: "payment_received", FulfillmentStep: 4, Quantity: 3,
			}},
		}}},
		balanceLines: []order.PreorderBalanceLine{{ShipmentID: "ship-1", OrderLineItemID: "line-1", ShopifyLineItemID: "gid://shopify/LineItem/21"}},
	}
	svc := &service{orderStore: store}

	err := svc.HandleFulfillment(context.Background(), ShopifyFulfillmentWebhook{
		ID:             99,
		OrderID:        1325,
		Status:         "success",
		TrackingNumber: "1Z999",
		ShipmentStatus: "in_transit",
		LineItems:      []ShopifyOrderLineItem{{ID: 21, VariantID: 501, Title: "After the Rain Shelf", Quantity: 3}},
	})
	if err != nil {
		t.Fatalf("HandleFulfillment returned error: %v", err)
	}
	if len(store.trackingCalls) != 1 || store.trackingCalls[0].itemID != "line-1" || store.trackingCalls[0].trackingNumber != "1Z999" {
		t.Fatalf("expected tracking on line-1, got %+v", store.trackingCalls)
	}
}
