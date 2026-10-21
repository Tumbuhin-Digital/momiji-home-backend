package order

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

// fulfillmentLinesStore serves balance lines and records what a fulfillment-order sync saves.
type fulfillmentLinesStore struct {
	*updateShippingWarehouseStore
	balanceLines []PreorderBalanceLine
	synced       []SyncedFulfillmentOrderLineItem
}

func (s *fulfillmentLinesStore) GetBalanceLinesByOrderID(context.Context, string) ([]PreorderBalanceLine, error) {
	return s.balanceLines, nil
}

func (s *fulfillmentLinesStore) UpsertFulfillmentOrder(_ context.Context, _, _, _ string, _ *string, items []SyncedFulfillmentOrderLineItem) error {
	s.synced = append(s.synced, items...)
	return nil
}

func ptr(v string) *string { return &v }

func foli(id, lineItemID string, remaining int) FulfillmentOrderLineItem {
	return FulfillmentOrderLineItem{ID: id, OrderLineItemID: "line-1", ShopifyLineItemID: ptr(lineItemID), RemainingQuantity: remaining}
}

// One item split across two batches has a product line per batch; each group must ship
// its own line even when the other has more units left.
func TestGroupFulfillmentLines_OrderEditGroupShipsItsOwnProductLine(t *testing.T) {
	method := BalanceMethodOrderEdit
	store := &fulfillmentLinesStore{
		updateShippingWarehouseStore: &updateShippingWarehouseStore{
			shipment: &PreorderShipment{ID: "ship-dec", BalanceMethod: &method},
		},
		balanceLines: []PreorderBalanceLine{
			{ShipmentID: "ship-oct", OrderLineItemID: "line-1", ShopifyLineItemID: "gid://shopify/LineItem/21"},
			{ShipmentID: "ship-dec", OrderLineItemID: "line-1", ShopifyLineItemID: "gid://shopify/LineItem/22"},
		},
	}
	svc := &service{store: store}
	folis := []FulfillmentOrderLineItem{
		foli("oct", "gid://shopify/LineItem/21", 2),
		foli("dec", "gid://shopify/LineItem/22", 1),
		foli("deposit", "gid://shopify/LineItem/11", 1),
	}

	got, err := svc.groupFulfillmentLines(context.Background(), "order-1", nil, folis)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "dec" {
		t.Fatalf("expected only the December product line, got %+v", got)
	}
}

func TestGroupFulfillmentLines_OtherGroupsNeverTakeAnEditLine(t *testing.T) {
	store := &fulfillmentLinesStore{
		updateShippingWarehouseStore: &updateShippingWarehouseStore{shipment: &PreorderShipment{ID: "ship-legacy"}},
		balanceLines: []PreorderBalanceLine{
			{ShipmentID: "ship-oct", OrderLineItemID: "line-1", ShopifyLineItemID: "gid://shopify/LineItem/21"},
		},
	}
	svc := &service{store: store}
	folis := []FulfillmentOrderLineItem{foli("oct", "gid://shopify/LineItem/21", 5), foli("deposit", "gid://shopify/LineItem/11", 1)}

	got, err := svc.groupFulfillmentLines(context.Background(), "order-1", nil, folis)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "deposit" {
		t.Fatalf("expected only the deposit line for the legacy group, got %+v", got)
	}
}

func TestGroupFulfillmentLines_EditGroupWithoutSyncedLinesIsRefused(t *testing.T) {
	method := BalanceMethodOrderEdit
	store := &fulfillmentLinesStore{
		updateShippingWarehouseStore: &updateShippingWarehouseStore{shipment: &PreorderShipment{ID: "ship-1", BalanceMethod: &method}},
		balanceLines: []PreorderBalanceLine{
			{ShipmentID: "ship-1", OrderLineItemID: "line-1", ShopifyLineItemID: "gid://shopify/LineItem/21"},
		},
	}
	svc := &service{store: store}

	_, err := svc.groupFulfillmentLines(context.Background(), "order-1", nil, []FulfillmentOrderLineItem{foli("deposit", "gid://shopify/LineItem/11", 1)})
	if errCode(err) != "balance_lines_not_synced" {
		t.Fatalf("expected balance_lines_not_synced, got %v", err)
	}
}

func TestGroupFulfillmentLines_OrdersWithoutEditsAreUnchanged(t *testing.T) {
	store := &fulfillmentLinesStore{updateShippingWarehouseStore: &updateShippingWarehouseStore{}}
	svc := &service{store: store}
	folis := []FulfillmentOrderLineItem{foli("a", "gid://shopify/LineItem/11", 1), {ID: "b", OrderLineItemID: "line-1"}}

	got, err := svc.groupFulfillmentLines(context.Background(), "order-1", nil, folis)
	if err != nil || len(got) != 2 {
		t.Fatalf("expected the lines untouched, got %+v err=%v", got, err)
	}
}

// The sync labels each fulfillment-order line: the edit's product line, the deposit line,
// and never a ship-ready line of the same variant.
func TestSyncFulfillmentOrders_LabelsProductAndDepositLines(t *testing.T) {
	shopifyOrderID := "1325"
	deposit, shipReady := "gid://shopify/LineItem/11", "gid://shopify/LineItem/15"
	title := "After the Rain Shelf (Deposit 50%)"
	store := &fulfillmentLinesStore{
		updateShippingWarehouseStore: &updateShippingWarehouseStore{order: &Order{
			ID:             "order-1",
			ShopifyOrderID: &shopifyOrderID,
			Items: []OrderItem{
				{ID: "line-1", Type: "pre_order", ShopifyVariantID: shelfVariant, Quantity: 3, Title: &title, ShopifyLineItemID: &deposit},
				{ID: "line-ready", Type: "ship_ready", ShopifyVariantID: shelfVariant, Quantity: 2, ShopifyLineItemID: &shipReady},
			},
		}},
		balanceLines: []PreorderBalanceLine{{ShipmentID: "ship-1", OrderLineItemID: "line-1", ShopifyLineItemID: "gid://shopify/LineItem/21"}},
	}
	shop := &shippingDepositShop{fos: []shopify.FulfillmentOrderData{{
		ID:     "fo-1",
		Status: "OPEN",
		LineItems: []shopify.FulfillmentOrderLineItemData{
			{ID: "foli-product", LineItemID: "gid://shopify/LineItem/21", VariantGID: shelfVariant, RemainingQuantity: 3},
			{ID: "foli-deposit", LineItemID: deposit, LineItemTitle: title, RemainingQuantity: 0},
			{ID: "foli-ready", LineItemID: shipReady, VariantGID: shelfVariant, RemainingQuantity: 2},
		},
	}}}
	svc := &service{store: store, shopClient: shop}

	if err := svc.SyncFulfillmentOrdersFromShopify(context.Background(), "order-1", shopifyOrderID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	kinds := map[string]string{}
	for _, s := range store.synced {
		kinds[s.ShopifyFulfillmentOrderLineItemID] = s.Kind
	}
	if kinds["foli-product"] != FOLIKindProduct || kinds["foli-deposit"] != FOLIKindDeposit {
		t.Fatalf("expected product and deposit labels, got %v", kinds)
	}
	if _, ok := kinds["foli-ready"]; ok {
		t.Fatal("a ship-ready line must not be synced as the pre-order item's line")
	}
}
