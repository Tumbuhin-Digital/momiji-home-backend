package webhook

import (
	"context"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
)

type trackingCall struct {
	itemID          string
	trackingNumber  string
	trackingURL     string
	trackingCompany string
	lastEvent       string
	itemStatus      string
	fulfillmentStep int
}

type fulfillmentTestStore struct {
	order           *order.Order
	trackingCalls   []trackingCall
	statusUpdates   []struct {
		aggregateStatus, financialStatus, fulfillmentStatus string
	}
	upsertFulfillment bool
}

func (s *fulfillmentTestStore) GetOrderByShopifyID(context.Context, string) (*order.Order, error) {
	return s.order, nil
}

func (s *fulfillmentTestStore) UpdateOrderItemTracking(_ context.Context, itemID, trackingNumber, trackingURL, trackingCompany, trackingLastEvent, itemStatus string, fulfillmentStep int, _ *time.Time) error {
	s.trackingCalls = append(s.trackingCalls, trackingCall{
		itemID:          itemID,
		trackingNumber:  trackingNumber,
		trackingURL:     trackingURL,
		trackingCompany: trackingCompany,
		lastEvent:       trackingLastEvent,
		itemStatus:      itemStatus,
		fulfillmentStep: fulfillmentStep,
	})
	return nil
}

func (s *fulfillmentTestStore) UpdateOrderStatus(_ context.Context, _ string, aggregateStatus, financialStatus, fulfillmentStatus string) error {
	s.statusUpdates = append(s.statusUpdates, struct {
		aggregateStatus, financialStatus, fulfillmentStatus string
	}{aggregateStatus, financialStatus, fulfillmentStatus})
	return nil
}

func (s *fulfillmentTestStore) UpdateOrderHoldUntilBatch(context.Context, string, *string) error {
	return nil
}

func (s *fulfillmentTestStore) GetNextFulfillmentSequence(context.Context, string) (int, error) {
	return 1, nil
}

func (s *fulfillmentTestStore) UpsertFulfillmentByShopifyID(context.Context, *order.Fulfillment, []order.FulfillmentLineItem) error {
	s.upsertFulfillment = true
	return nil
}

func (s *fulfillmentTestStore) GetFOLIByOrderLineItemIDs(context.Context, string, []string) ([]order.FulfillmentOrderLineItem, error) {
	return nil, nil
}

func (s *fulfillmentTestStore) DecrementFOLIRemaining(context.Context, string, int) error {
	return nil
}

func (s *fulfillmentTestStore) CreateOrder(context.Context, *order.Order) error { return nil }
func (s *fulfillmentTestStore) GetOrder(context.Context, string, string) (*order.Order, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) GetOrdersByCustomer(context.Context, string, order.OrderQuery) ([]order.Order, int64, error) {
	return nil, 0, nil
}
func (s *fulfillmentTestStore) GetAllOrdersForExport(context.Context, order.OrderQuery) ([]order.Order, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) UpdateItemStatusByType(context.Context, string, string, string) error {
	return nil
}
func (s *fulfillmentTestStore) UpdateItemStatusByID(context.Context, string, string) error { return nil }
func (s *fulfillmentTestStore) UpdateItemStepByType(context.Context, string, string, int) error {
	return nil
}
func (s *fulfillmentTestStore) UpdateOrderItemStep(context.Context, string, int) error { return nil }
func (s *fulfillmentTestStore) UpdateOrderItemReceived(context.Context, string, int, int) error {
	return nil
}
func (s *fulfillmentTestStore) GetPreorderShipment(context.Context, string) (*order.PreorderShipment, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) GetPreorderShipments(context.Context, string) ([]order.PreorderShipment, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) GetPreorderShipmentByBatch(context.Context, string, *string) (*order.PreorderShipment, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) UpsertPreorderShipment(context.Context, *order.PreorderShipment, []order.PreorderPackingItem) error {
	return nil
}
func (s *fulfillmentTestStore) UpdatePreorderShipping(context.Context, string, float64, string, float64) error {
	return nil
}
func (s *fulfillmentTestStore) UpdatePreorderShippingByShipmentID(context.Context, string, float64, string, float64) error {
	return nil
}
func (s *fulfillmentTestStore) MarkPreorderInvoiceSent(context.Context, string, time.Time) error {
	return nil
}
func (s *fulfillmentTestStore) MarkPreorderShipmentInvoiceSent(context.Context, string, string, string, time.Time, float64) error {
	return nil
}
func (s *fulfillmentTestStore) MarkPreorderShipmentInvoicePaid(context.Context, string, time.Time) error {
	return nil
}
func (s *fulfillmentTestStore) GetPreorderShipmentByDraftOrderID(context.Context, string) (*order.PreorderShipment, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) GetPreorderShipmentByID(context.Context, string) (*order.PreorderShipment, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) HasAnyShipmentInvoiceForOrder(context.Context, string) (bool, error) {
	return false, nil
}
func (s *fulfillmentTestStore) GetVariantDimensions(context.Context, []string) (map[string]order.VariantDimensions, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) GetUSZipStateAbbr(context.Context, string) (string, bool) {
	return "", false
}
func (s *fulfillmentTestStore) UpsertFulfillmentOrder(context.Context, string, string, string, *string, []order.SyncedFulfillmentOrderLineItem) error {
	return nil
}
func (s *fulfillmentTestStore) GetFulfillmentOrdersByOrderID(context.Context, string) ([]order.FulfillmentOrder, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) CreateFulfillment(context.Context, *order.Fulfillment) error { return nil }
func (s *fulfillmentTestStore) GetFulfillmentsByOrderID(context.Context, string) ([]order.Fulfillment, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) GetFulfillmentByID(context.Context, string) (*order.Fulfillment, error) {
	return nil, nil
}
func (s *fulfillmentTestStore) MarkFulfillmentDelivered(context.Context, string, time.Time) error {
	return nil
}
func (s *fulfillmentTestStore) RecordWebhookEvent(context.Context, string, string) (bool, error) {
	return false, nil
}
func (s *fulfillmentTestStore) IsWebhookProcessed(context.Context, string) (bool, error) {
	return false, nil
}
func (s *fulfillmentTestStore) SaveWebhookEvent(context.Context, string, string) error { return nil }

var _ order.Store = (*fulfillmentTestStore)(nil)

func TestHandleFulfillmentShipReadyShipped(t *testing.T) {
	title := "test lengkap dimensi ready"
	store := &fulfillmentTestStore{
		order: &order.Order{
			ID:              "order-1",
			FinancialStatus: "paid",
			AggregateStatus: "processing",
			Items: []order.OrderItem{
				{
					ID:               "item-ship-ready",
					Type:             "ship_ready",
					ShopifyVariantID: "gid://shopify/ProductVariant/48731155988735",
					Title:            &title,
					ItemStatus:       "paid",
					FulfillmentStep:  1,
				},
			},
		},
	}

	svc := &service{orderStore: store}
	err := svc.HandleFulfillment(context.Background(), ShopifyFulfillmentWebhook{
		ID:              6330690502911,
		OrderID:         6969570459903,
		Status:          "success",
		TrackingCompany: "UPS",
		TrackingNumber:  "1Z999",
		TrackingURLs:    []string{"https://tracking.example/1Z999"},
		ShipmentStatus:  "in_transit",
		LineItems: []ShopifyOrderLineItem{
			{
				VariantID: 48731155988735,
				Title:     title,
				Quantity:  1,
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleFulfillment returned error: %v", err)
	}

	if len(store.trackingCalls) != 1 {
		t.Fatalf("expected 1 tracking update, got %d", len(store.trackingCalls))
	}
	call := store.trackingCalls[0]
	if call.itemID != "item-ship-ready" {
		t.Fatalf("unexpected item id: %s", call.itemID)
	}
	if call.itemStatus != "shipped" || call.fulfillmentStep != 3 {
		t.Fatalf("expected shipped step 3, got status=%s step=%d", call.itemStatus, call.fulfillmentStep)
	}
	if store.upsertFulfillment {
		t.Fatal("ship_ready fulfillment should not create fulfillment records")
	}
	if len(store.statusUpdates) != 1 || store.statusUpdates[0].aggregateStatus != "on_progress" {
		t.Fatalf("expected on_progress status update, got %+v", store.statusUpdates)
	}
}

func TestHandleFulfillmentShipReadyDelivered(t *testing.T) {
	title := "test lengkap dimensi ready"
	store := &fulfillmentTestStore{
		order: &order.Order{
			ID:              "order-1",
			FinancialStatus: "paid",
			AggregateStatus: "on_progress",
			Items: []order.OrderItem{
				{
					ID:               "item-ship-ready",
					Type:             "ship_ready",
					ShopifyVariantID: "gid://shopify/ProductVariant/48731155988735",
					Title:            &title,
					ItemStatus:       "shipped",
					FulfillmentStep:  3,
				},
			},
		},
	}

	svc := &service{orderStore: store}
	err := svc.HandleFulfillment(context.Background(), ShopifyFulfillmentWebhook{
		ID:             6330690502911,
		OrderID:        6969570459903,
		Status:         "success",
		ShipmentStatus: "delivered",
		LineItems: []ShopifyOrderLineItem{
			{
				VariantID: 48731155988735,
				Title:     title,
				Quantity:  1,
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleFulfillment returned error: %v", err)
	}

	if len(store.trackingCalls) != 1 {
		t.Fatalf("expected 1 tracking update, got %d", len(store.trackingCalls))
	}
	call := store.trackingCalls[0]
	if call.itemStatus != "delivered" || call.fulfillmentStep != 4 {
		t.Fatalf("expected delivered step 4, got status=%s step=%d", call.itemStatus, call.fulfillmentStep)
	}
	if len(store.statusUpdates) != 1 || store.statusUpdates[0].aggregateStatus != "completed" {
		t.Fatalf("expected completed status update, got %+v", store.statusUpdates)
	}
}

func TestHandleFulfillmentPreOrderStillCreatesFulfillment(t *testing.T) {
	title := "preorder item"
	store := &fulfillmentTestStore{
		order: &order.Order{
			ID:              "order-2",
			FinancialStatus: "paid",
			AggregateStatus: "on_progress",
			Items: []order.OrderItem{
				{
					ID:               "item-pre-order",
					Type:             "pre_order",
					ShopifyVariantID: "gid://shopify/ProductVariant/123",
					Title:            &title,
					ItemStatus:       "payment_received",
					FulfillmentStep:  4,
				},
			},
			Customer: &customer.Customer{},
		},
	}

	svc := &service{orderStore: store}
	err := svc.HandleFulfillment(context.Background(), ShopifyFulfillmentWebhook{
		ID:             99,
		OrderID:        100,
		Status:         "success",
		TrackingNumber: "TRACK123",
		ShipmentStatus: "in_transit",
		LineItems: []ShopifyOrderLineItem{
			{
				VariantID: 123,
				Title:     title,
				Quantity:  1,
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleFulfillment returned error: %v", err)
	}

	if !store.upsertFulfillment {
		t.Fatal("expected pre_order fulfillment record to be upserted")
	}
	if len(store.trackingCalls) != 1 || store.trackingCalls[0].fulfillmentStep != 4 {
		t.Fatalf("expected pre_order shipped step 4, got %+v", store.trackingCalls)
	}
}
