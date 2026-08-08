package order

import (
	"context"
	"testing"
	"time"
)

func TestResolveWarehouseOrigin(t *testing.T) {
	t.Run("empty defaults to east", func(t *testing.T) {
		if got := resolveWarehouseOrigin(nil); got != "east" {
			t.Fatalf("expected east, got %q", got)
		}
		if got := resolveWarehouseOrigin(&PreorderShipment{}); got != "east" {
			t.Fatalf("expected east for empty origin, got %q", got)
		}
	})

	t.Run("preserves west", func(t *testing.T) {
		got := resolveWarehouseOrigin(&PreorderShipment{WarehouseOrigin: "west"})
		if got != "west" {
			t.Fatalf("expected west, got %q", got)
		}
	})
}

func TestBuildPreorderShipmentUpsertUpdates_OmitsEmptyWarehouseOrigin(t *testing.T) {
	estimate := 69.75
	weight := 12.5
	updates := buildPreorderShipmentUpsertUpdates(&PreorderShipment{
		TotalBoxes:        3,
		TotalWeightLb:     &weight,
		EstimatedShipping: &estimate,
	})

	if _, ok := updates["warehouse_origin"]; ok {
		t.Fatal("expected warehouse_origin to be omitted when empty")
	}
	if updates["total_boxes"] != 3 {
		t.Fatalf("expected total_boxes 3, got %v", updates["total_boxes"])
	}
}

func TestBuildPreorderShipmentUpsertUpdates_IncludesWarehouseOrigin(t *testing.T) {
	updates := buildPreorderShipmentUpsertUpdates(&PreorderShipment{
		TotalBoxes:      2,
		WarehouseOrigin: "west",
	})

	if updates["warehouse_origin"] != "west" {
		t.Fatalf("expected west, got %v", updates["warehouse_origin"])
	}
}

type updateShippingWarehouseStore struct {
	order            *Order
	shipment         *PreorderShipment
	upsertedShipment *PreorderShipment
}

func (s *updateShippingWarehouseStore) GetOrder(context.Context, string, string) (*Order, error) {
	return s.order, nil
}

func (s *updateShippingWarehouseStore) GetPreorderShipment(context.Context, string) (*PreorderShipment, error) {
	return s.shipment, nil
}

func (s *updateShippingWarehouseStore) GetPreorderShipments(context.Context, string) ([]PreorderShipment, error) {
	if s.shipment == nil {
		return nil, nil
	}
	return []PreorderShipment{*s.shipment}, nil
}

func (s *updateShippingWarehouseStore) GetPreorderShipmentByBatch(_ context.Context, _ string, _ *string) (*PreorderShipment, error) {
	return s.shipment, nil
}

func (s *updateShippingWarehouseStore) GetVariantDimensions(context.Context, []string) (map[string]VariantDimensions, error) {
	return map[string]VariantDimensions{
		"var-1": {ShopifyVariantID: "var-1", WeightKg: 5, WidthCm: 40, HeightCm: 20, DepthCm: 30},
	}, nil
}

func (s *updateShippingWarehouseStore) UpsertPreorderShipment(_ context.Context, shipment *PreorderShipment, _ []PreorderPackingItem) error {
	if shipment.ID == "" {
		shipment.ID = "ship-1"
	}
	s.upsertedShipment = shipment
	s.shipment = shipment
	return nil
}

func (s *updateShippingWarehouseStore) UpdatePreorderShipping(context.Context, string, float64, string, float64) error {
	return nil
}

func (s *updateShippingWarehouseStore) UpdatePreorderShippingByShipmentID(_ context.Context, shipmentID string, finalPrice float64, notes string, creditAmount float64) error {
	if s.shipment != nil {
		s.shipment.ID = shipmentID
		s.shipment.FinalShippingPrice = &finalPrice
		s.shipment.ShippingNotes = &notes
		s.shipment.CreditAmount = creditAmount
	}
	return nil
}

func (s *updateShippingWarehouseStore) UpdateItemStepByType(context.Context, string, string, int) error {
	return nil
}

func (s *updateShippingWarehouseStore) CreateOrder(context.Context, *Order) error { return nil }
func (s *updateShippingWarehouseStore) GetOrderByShopifyID(context.Context, string) (*Order, error) {
	return nil, nil
}
func (s *updateShippingWarehouseStore) GetOrdersByCustomer(context.Context, string, OrderQuery) ([]Order, int64, error) {
	return nil, 0, nil
}
func (s *updateShippingWarehouseStore) GetAllOrdersForExport(context.Context, OrderQuery) ([]Order, error) {
	return nil, nil
}
func (s *updateShippingWarehouseStore) UpdateOrderStatus(context.Context, string, string, string, string) error {
	return nil
}
func (s *updateShippingWarehouseStore) UpdateOrderHoldUntilBatch(context.Context, string, *string) error {
	return nil
}
func (s *updateShippingWarehouseStore) UpdateItemStatusByType(context.Context, string, string, string) error {
	return nil
}
func (s *updateShippingWarehouseStore) UpdateItemStatusByID(context.Context, string, string) error {
	return nil
}
func (s *updateShippingWarehouseStore) UpdateOrderItemStep(_ context.Context, itemID string, step int) error {
	for i := range s.order.Items {
		if s.order.Items[i].ID == itemID {
			s.order.Items[i].FulfillmentStep = step
			break
		}
	}
	return nil
}
func (s *updateShippingWarehouseStore) UpdateOrderItemTracking(context.Context, string, string, string, string, string, string, int, *time.Time) error {
	return nil
}
func (s *updateShippingWarehouseStore) UpdateOrderItemReceived(context.Context, string, int, int) error {
	return nil
}
func (s *updateShippingWarehouseStore) MarkPreorderInvoiceSent(context.Context, string, time.Time) error {
	return nil
}
func (s *updateShippingWarehouseStore) MarkPreorderShipmentInvoiceSent(context.Context, string, string, string, time.Time) error {
	return nil
}
func (s *updateShippingWarehouseStore) MarkPreorderShipmentInvoicePaid(context.Context, string, time.Time) error {
	return nil
}
func (s *updateShippingWarehouseStore) GetPreorderShipmentByDraftOrderID(context.Context, string) (*PreorderShipment, error) {
	return nil, nil
}
func (s *updateShippingWarehouseStore) GetPreorderShipmentByID(context.Context, string) (*PreorderShipment, error) {
	return nil, nil
}
func (s *updateShippingWarehouseStore) HasAnyShipmentInvoiceForOrder(context.Context, string) (bool, error) {
	return false, nil
}
func (s *updateShippingWarehouseStore) GetUSZipStateAbbr(context.Context, string) (string, bool) {
	return "", false
}
func (s *updateShippingWarehouseStore) UpsertFulfillmentOrder(context.Context, string, string, string, *string, []SyncedFulfillmentOrderLineItem) error {
	return nil
}
func (s *updateShippingWarehouseStore) GetFulfillmentOrdersByOrderID(context.Context, string) ([]FulfillmentOrder, error) {
	return nil, nil
}
func (s *updateShippingWarehouseStore) GetFOLIByOrderLineItemIDs(context.Context, string, []string) ([]FulfillmentOrderLineItem, error) {
	return nil, nil
}
func (s *updateShippingWarehouseStore) DecrementFOLIRemaining(context.Context, string, int) error {
	return nil
}
func (s *updateShippingWarehouseStore) CreateFulfillment(context.Context, *Fulfillment) error {
	return nil
}
func (s *updateShippingWarehouseStore) UpsertFulfillmentByShopifyID(context.Context, *Fulfillment, []FulfillmentLineItem) error {
	return nil
}
func (s *updateShippingWarehouseStore) GetFulfillmentsByOrderID(context.Context, string) ([]Fulfillment, error) {
	return nil, nil
}
func (s *updateShippingWarehouseStore) GetFulfillmentByID(context.Context, string) (*Fulfillment, error) {
	return nil, nil
}
func (s *updateShippingWarehouseStore) GetNextFulfillmentSequence(context.Context, string) (int, error) {
	return 0, nil
}
func (s *updateShippingWarehouseStore) MarkFulfillmentDelivered(context.Context, string, time.Time) error {
	return nil
}
func (s *updateShippingWarehouseStore) RecordWebhookEvent(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *updateShippingWarehouseStore) IsWebhookProcessed(context.Context, string) (bool, error) {
	return false, nil
}
func (s *updateShippingWarehouseStore) SaveWebhookEvent(context.Context, string, string) error {
	return nil
}

var _ Store = (*updateShippingWarehouseStore)(nil)

func TestUpdatePreorderShipping_AllowsManualPriceWithoutEstimate(t *testing.T) {
	lineID := "line-1"
	store := &updateShippingWarehouseStore{
		order: &Order{
			ID: "order-1",
			Items: []OrderItem{
				{ID: lineID, Type: "pre_order", ShopifyVariantID: "var-1", Quantity: 2, FulfillmentStep: 1},
			},
		},
		shipment: nil,
	}

	svc := &service{store: store}
	dto, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{
		FinalShippingPrice: 450,
		ShippingNotes:      "LTL quote",
		Packing: []PackingItemDTO{
			{LineItemID: lineID, BoxCount: 2, IsNested: false},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.upsertedShipment == nil {
		t.Fatal("expected shipment upsert for LTL manual price")
	}
	if store.upsertedShipment.EstimatedShipping != nil {
		t.Fatalf("expected nil estimate for LTL, got %v", *store.upsertedShipment.EstimatedShipping)
	}
	if dto == nil || dto.FinalShippingPrice == nil || *dto.FinalShippingPrice != "450.00" {
		t.Fatalf("expected final shipping 450.00, got %+v", dto)
	}
	if store.order.Items[0].FulfillmentStep != preOrderStepShippingConfigured {
		t.Fatalf("expected step %d, got %d", preOrderStepShippingConfigured, store.order.Items[0].FulfillmentStep)
	}
}

func TestUpdatePreorderShipping_PreservesWarehouseOriginOnPackingUpsert(t *testing.T) {
	estimate := 69.75
	lineID := "line-1"
	store := &updateShippingWarehouseStore{
		order: &Order{
			ID: "order-1",
			Items: []OrderItem{
				{ID: lineID, Type: "pre_order", ShopifyVariantID: "var-1", Quantity: 2, FulfillmentStep: 1},
			},
		},
		shipment: &PreorderShipment{
			ID:                "ship-1",
			OrderID:           "order-1",
			EstimatedShipping: &estimate,
			WarehouseOrigin:   "west",
		},
	}

	svc := &service{store: store}
	_, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{
		FinalShippingPrice: 70,
		Packing: []PackingItemDTO{
			{LineItemID: lineID, BoxCount: 2, IsNested: false},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.upsertedShipment == nil {
		t.Fatal("expected UpsertPreorderShipment to be called")
	}
	if store.upsertedShipment.WarehouseOrigin != "west" {
		t.Fatalf("expected warehouse origin west, got %q", store.upsertedShipment.WarehouseOrigin)
	}
}
