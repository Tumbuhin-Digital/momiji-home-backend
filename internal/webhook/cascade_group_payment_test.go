package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
)

type cascadeOrderStore struct {
	fulfillmentTestStore
	shipments     []order.PreorderShipment
	statusByID    map[string]string
	stepByID      map[string]int
	statusUpdates []struct{ itemID, status string }
	stepUpdates   []struct{ itemID string; step int }
}

func (s *cascadeOrderStore) GetOrder(context.Context, string, string) (*order.Order, error) {
	return s.order, nil
}

func (s *cascadeOrderStore) GetPreorderShipments(context.Context, string) ([]order.PreorderShipment, error) {
	return s.shipments, nil
}

func (s *cascadeOrderStore) UpdateItemStatusByID(_ context.Context, itemID, status string) error {
	if s.statusByID == nil {
		s.statusByID = map[string]string{}
	}
	s.statusByID[itemID] = status
	s.statusUpdates = append(s.statusUpdates, struct{ itemID, status string }{itemID, status})
	return nil
}

func (s *cascadeOrderStore) UpdateOrderItemStep(_ context.Context, itemID string, step int) error {
	if s.stepByID == nil {
		s.stepByID = map[string]int{}
	}
	s.stepByID[itemID] = step
	s.stepUpdates = append(s.stepUpdates, struct {
		itemID string
		step   int
	}{itemID, step})
	return nil
}

type cascadePreorderStore struct {
	settlements map[string]*preorder.Settlement
	paidIDs     []string
}

func (s *cascadePreorderStore) CreateSettlement(context.Context, *preorder.Settlement) error {
	return nil
}
func (s *cascadePreorderStore) GetSettlementByID(context.Context, string) (*preorder.Settlement, error) {
	return nil, nil
}
func (s *cascadePreorderStore) GetSettlementByOrderLineItemID(_ context.Context, itemID string) (*preorder.Settlement, error) {
	if s.settlements == nil {
		return nil, nil
	}
	return s.settlements[itemID], nil
}
func (s *cascadePreorderStore) ListSettlements(context.Context, preorder.SettlementFilter) ([]preorder.PreorderRow, int64, error) {
	return nil, 0, nil
}
func (s *cascadePreorderStore) GetAllSettlementsForExport(context.Context, preorder.SettlementFilter) ([]preorder.PreorderRow, error) {
	return nil, nil
}
func (s *cascadePreorderStore) UpdateSettlementStatus(_ context.Context, id, status string, _ *time.Time) error {
	if status == "paid" {
		s.paidIDs = append(s.paidIDs, id)
	}
	return nil
}
func (s *cascadePreorderStore) MarkSettlementsInvoiced(context.Context, []string, string, string, time.Time) error {
	return nil
}
func (s *cascadePreorderStore) AllSettlementsPaid(context.Context, string) (bool, error) {
	return false, nil
}
func (s *cascadePreorderStore) GetSettlementsForReminder(context.Context, int) ([]preorder.PreorderRow, error) {
	return nil, nil
}

var _ preorder.PreorderStore = (*cascadePreorderStore)(nil)

func TestCascadeGroupShipmentPayments_AdvancesPaidBatchWhileSiblingUnpaid(t *testing.T) {
	now := time.Now()
	paidAt := now
	sentAt := now

	store := &cascadeOrderStore{
		fulfillmentTestStore: fulfillmentTestStore{
			order: &order.Order{
				ID: "order-1",
				Items: []order.OrderItem{
					{ID: "line-dec", Type: "pre_order", Quantity: 5, FulfillmentStep: 3, ItemStatus: "waiting_payment"},
					{ID: "line-oct", Type: "pre_order", Quantity: 4, FulfillmentStep: 3, ItemStatus: "waiting_payment"},
					{ID: "line-unbatched", Type: "pre_order", Quantity: 1, FulfillmentStep: 2, ItemStatus: "paid"},
				},
			},
		},
		shipments: []order.PreorderShipment{
			{
				ID:            "ship-dec",
				InvoicePaidAt: &paidAt,
				PackingItems:  []order.PreorderPackingItem{{OrderLineItemID: "line-dec", Quantity: 5}},
			},
			{
				ID:            "ship-oct",
				InvoiceSentAt: &sentAt,
				PackingItems:  []order.PreorderPackingItem{{OrderLineItemID: "line-oct", Quantity: 4}},
			},
		},
	}

	preorderStore := &cascadePreorderStore{
		settlements: map[string]*preorder.Settlement{
			"line-dec": {ID: "st-dec", OrderLineItemID: "line-dec", Status: "invoiced"},
			"line-oct": {ID: "st-oct", OrderLineItemID: "line-oct", Status: "invoiced"},
		},
	}

	svc := &service{orderStore: store, preorderStore: preorderStore}
	if err := svc.cascadeGroupShipmentPayments(context.Background(), "order-1"); err != nil {
		t.Fatalf("cascadeGroupShipmentPayments: %v", err)
	}

	if store.statusByID["line-dec"] != "payment_received" {
		t.Fatalf("expected december line payment_received, got %q", store.statusByID["line-dec"])
	}
	if store.stepByID["line-dec"] != 4 {
		t.Fatalf("expected december line step 4, got %d", store.stepByID["line-dec"])
	}
	if _, ok := store.statusByID["line-oct"]; ok {
		t.Fatal("unpaid oktober line must not be advanced")
	}
	if _, ok := store.statusByID["line-unbatched"]; ok {
		t.Fatal("unpaid unbatched line must not be advanced")
	}
	if len(preorderStore.paidIDs) != 1 || preorderStore.paidIDs[0] != "st-dec" {
		t.Fatalf("expected only december settlement marked paid, got %v", preorderStore.paidIDs)
	}
}
