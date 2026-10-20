package order

import (
	"context"
	"errors"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

var shippingDepositAttrs = []shopify.AttributeInput{{Key: "charge_type", Value: preorderShippingDepositChargeType}}

func depositOrderFulfillmentOrders() []shopify.FulfillmentOrderData {
	return []shopify.FulfillmentOrderData{
		{
			ID:     "fo-1",
			Status: "IN_PROGRESS", // the pre-order goods were just fulfilled with tracking
			LineItems: []shopify.FulfillmentOrderLineItemData{
				{ID: "foli-product", RemainingQuantity: 0},
				{ID: "foli-shipping", RemainingQuantity: 1, CustomAttributes: shippingDepositAttrs},
			},
		},
		{
			ID:        "fo-closed",
			Status:    "CLOSED",
			LineItems: []shopify.FulfillmentOrderLineItemData{{ID: "foli-closed", RemainingQuantity: 1, CustomAttributes: shippingDepositAttrs}},
		},
	}
}

func TestOpenShippingDepositLines_OnlyOpenUpfrontShippingLines(t *testing.T) {
	got := openShippingDepositLines(depositOrderFulfillmentOrders())

	if len(got) != 1 || got[0].FulfillmentOrderID != "fo-1" {
		t.Fatalf("expected only fo-1, got %+v", got)
	}
	lines := got[0].FulfillmentOrderLineItems
	if len(lines) != 1 || lines[0].ID != "foli-shipping" || lines[0].Quantity != 1 {
		t.Fatalf("expected only the shipping deposit line, got %+v", lines)
	}
}

func TestOpenShippingDepositLines_NothingLeftIsANoop(t *testing.T) {
	fos := []shopify.FulfillmentOrderData{{
		ID:        "fo-1",
		Status:    "OPEN",
		LineItems: []shopify.FulfillmentOrderLineItemData{{ID: "foli-shipping", RemainingQuantity: 0, CustomAttributes: shippingDepositAttrs}},
	}}
	if got := openShippingDepositLines(fos); len(got) != 0 {
		t.Fatalf("expected nothing to fulfill, got %+v", got)
	}
}

// shippingDepositShop serves fulfillment orders and records fulfillments; any other
// Shopify call panics on the nil embedded client.
type shippingDepositShop struct {
	shopify.Client
	fos       []shopify.FulfillmentOrderData
	fetchErr  error
	fulfilled []shopify.CreateFulfillmentV2Input
}

func (c *shippingDepositShop) FetchFulfillmentOrders(context.Context, string) ([]shopify.FulfillmentOrderData, error) {
	return c.fos, c.fetchErr
}

func (c *shippingDepositShop) CreateFulfillmentV2(_ context.Context, input shopify.CreateFulfillmentV2Input) (*shopify.CreateFulfillmentV2Result, error) {
	c.fulfilled = append(c.fulfilled, input)
	return &shopify.CreateFulfillmentV2Result{FulfillmentID: "gid://shopify/Fulfillment/2"}, nil
}

func TestFulfillPreorderShippingDeposit_NoTrackingAndNoCustomerEmail(t *testing.T) {
	shop := &shippingDepositShop{fos: depositOrderFulfillmentOrders()}
	svc := &service{shopClient: shop}

	svc.fulfillPreorderShippingDeposit(context.Background(), "order-1", "1325")

	if len(shop.fulfilled) != 1 {
		t.Fatalf("expected one fulfillment, got %d", len(shop.fulfilled))
	}
	in := shop.fulfilled[0]
	if in.NotifyCustomer {
		t.Fatal("closing the shipping deposit line must not email the customer")
	}
	if in.TrackingNumber != "" {
		t.Fatalf("shipping deposit line must carry no tracking, got %q", in.TrackingNumber)
	}
}

func TestFulfillPreorderShippingDeposit_FetchFailureIsIgnored(t *testing.T) {
	shop := &shippingDepositShop{fetchErr: errors.New("shopify down")}
	svc := &service{shopClient: shop}

	svc.fulfillPreorderShippingDeposit(context.Background(), "order-1", "1325")

	if len(shop.fulfilled) != 0 {
		t.Fatalf("expected no fulfillment attempt, got %d", len(shop.fulfilled))
	}
}
