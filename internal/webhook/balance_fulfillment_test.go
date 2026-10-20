package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
)

// balanceOrderStore serves a paid balance order: Shopify's order is unknown to the app,
// and its shipment_id resolves to the app order that holds the deposit.
type balanceOrderStore struct {
	cascadeOrderStore
}

func (s *balanceOrderStore) GetOrderByShopifyID(context.Context, string) (*order.Order, error) {
	return nil, nil
}

func (s *balanceOrderStore) GetPreorderShipmentByID(_ context.Context, id string) (*order.PreorderShipment, error) {
	return &order.PreorderShipment{ID: id, OrderID: "order-1"}, nil
}

type fulfillRecordingShop struct {
	product.MockShopifyClientFunc
	fulfilled []string
	err       error
}

func (c *fulfillRecordingShop) CreateFulfillment(_ context.Context, shopifyOrderID string) error {
	c.fulfilled = append(c.fulfilled, shopifyOrderID)
	return c.err
}

func balancePaidPayload() ShopifyOrderWebhook {
	return ShopifyOrderWebhook{
		ID:             7001,
		OrderNumber:    1324,
		Email:          "buyer@example.com",
		Currency:       "USD",
		Customer:       ShopifyCustomer{ID: 1, Email: "buyer@example.com"},
		NoteAttributes: []ShopifyProperty{{Name: "source", Value: "wholesale"}},
		LineItems: []ShopifyOrderLineItem{{
			Title:    "Alaya House Shelf (Remaining 50%)",
			Quantity: 1,
			Price:    "149.50",
			Properties: []ShopifyProperty{
				{Name: "charge_type", Value: "pre_order_group_balance"},
				{Name: "shipment_id", Value: "ship-1"},
			},
		}},
	}
}

func balanceWebhookService(shop *fulfillRecordingShop) *service {
	store := &balanceOrderStore{}
	store.order = &order.Order{ID: "order-1"}
	return &service{
		orderStore:    store,
		authStore:     auth.NewMockAuthStore(),
		preorderStore: &cascadePreorderStore{},
		customerStore: stubCustomerStore{},
		shopClient:    shop,
	}
}

func TestHandleOrderPaid_BalanceOrderIsFulfilledInShopify(t *testing.T) {
	shop := &fulfillRecordingShop{}
	svc := balanceWebhookService(shop)

	if err := svc.HandleOrderPaid(context.Background(), balancePaidPayload()); err != nil {
		t.Fatalf("HandleOrderPaid returned error: %v", err)
	}
	if len(shop.fulfilled) != 1 || shop.fulfilled[0] != "7001" {
		t.Fatalf("expected balance order 7001 fulfilled once, got %v", shop.fulfilled)
	}
}

func TestHandleOrderPaid_BalanceFulfillFailureDoesNotFailWebhook(t *testing.T) {
	shop := &fulfillRecordingShop{err: errors.New("no fulfillment orders yet")}
	svc := balanceWebhookService(shop)

	if err := svc.HandleOrderPaid(context.Background(), balancePaidPayload()); err != nil {
		t.Fatalf("a failed fulfill must not fail the paid webhook, got %v", err)
	}
	if len(shop.fulfilled) != 1 {
		t.Fatalf("expected one fulfill attempt, got %d", len(shop.fulfilled))
	}
}
