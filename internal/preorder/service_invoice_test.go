package preorder

import (
	"context"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

type recordingShopClient struct {
	lastDraftInput shopify.DraftOrderInput
}

func (c *recordingShopClient) QueryAdminGraphQL(context.Context, string, map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (c *recordingShopClient) CreateDraftOrder(_ context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	c.lastDraftInput = input
	return &shopify.DraftOrderResponse{
		ID:         "gid://shopify/DraftOrder/1",
		InvoiceUrl: "https://example.com/invoice",
	}, nil
}

func (c *recordingShopClient) CreateStorefrontCart(context.Context, shopify.CartCreateInput) (*shopify.CartCreateResponse, error) {
	return nil, nil
}

func (c *recordingShopClient) CreateRefund(context.Context, string, float64, string, string) error {
	return nil
}

func (c *recordingShopClient) GetVariantsInventory(context.Context, []string) (map[string]int, error) {
	return nil, nil
}

func (c *recordingShopClient) CreateFulfillment(context.Context, string) error { return nil }

func (c *recordingShopClient) FetchFulfillmentOrders(context.Context, string) ([]shopify.FulfillmentOrderData, error) {
	return nil, nil
}

func (c *recordingShopClient) CreateFulfillmentV2(context.Context, shopify.CreateFulfillmentV2Input) (*shopify.CreateFulfillmentV2Result, error) {
	return nil, nil
}

func (c *recordingShopClient) CreateFulfillmentEvent(context.Context, string, string) error { return nil }

type invoiceTestStore struct {
	settlements map[string]Settlement
}

func (s *invoiceTestStore) CreateSettlement(context.Context, *Settlement) error { return nil }

func (s *invoiceTestStore) GetSettlementByID(_ context.Context, id string) (*Settlement, error) {
	st, ok := s.settlements[id]
	if !ok {
		return nil, nil
	}
	return &st, nil
}

func (s *invoiceTestStore) GetSettlementByOrderLineItemID(_ context.Context, itemID string) (*Settlement, error) {
	for _, st := range s.settlements {
		if st.OrderLineItemID == itemID {
			copy := st
			return &copy, nil
		}
	}
	return nil, nil
}

func (s *invoiceTestStore) ListSettlements(context.Context, SettlementFilter) ([]PreorderRow, int64, error) {
	return nil, 0, nil
}

func (s *invoiceTestStore) GetAllSettlementsForExport(context.Context, SettlementFilter) ([]PreorderRow, error) {
	return nil, nil
}

func (s *invoiceTestStore) UpdateSettlementStatus(_ context.Context, id, status string, ts *time.Time) error {
	st := s.settlements[id]
	st.Status = status
	st.InvoicedAt = ts
	s.settlements[id] = st
	return nil
}

func (s *invoiceTestStore) AllSettlementsPaid(context.Context, string) (bool, error) { return false, nil }

func (s *invoiceTestStore) GetSettlementsForReminder(context.Context, int) ([]PreorderRow, error) {
	return nil, nil
}

type noopEmailService struct{}

func (noopEmailService) SendOrderConfirmation(context.Context, string, email.OrderEmailData) error {
	return nil
}
func (noopEmailService) SendInvoice(context.Context, string, email.SettlementEmailData) error { return nil }
func (noopEmailService) SendSettlementPaid(context.Context, string, email.SettlementEmailData) error {
	return nil
}
func (noopEmailService) SendReminder(context.Context, string, email.SettlementEmailData) error { return nil }
func (noopEmailService) SendExpired(context.Context, string, email.SettlementEmailData) error { return nil }
func (noopEmailService) SendShipmentDispatched(context.Context, string, email.ShipmentEmailData) error {
	return nil
}

func TestInvoiceSettlementsWithShippingAddsShippingLineItem(t *testing.T) {
	store := &invoiceTestStore{
		settlements: map[string]Settlement{
			"settle-1": {
				ID:              "settle-1",
				OrderLineItemID: "line-1",
				OrderID:         "order-1",
				Title:           "test item",
				BalanceAmount:   50,
				Status:          "pending",
				CustomerEmail:   "buyer@example.com",
				CustomerName:    "Buyer",
			},
		},
	}
	shop := &recordingShopClient{}

	svc := &service{
		store:        store,
		shopClient:   shop,
		emailService: noopEmailService{},
	}

	_, err := svc.InvoiceSettlementsWithShipping(context.Background(), []string{"line-1"}, InvoiceOptions{
		ShippingTitle: "UPS Ground",
		ShippingPrice: 17.50,
		ShippingAddress: &shopify.AddressInput{
			FirstName: "Gilang",
			LastName:  "Aryo",
			Address1:  "4800 Osuna Rd NE",
			City:      "Albuquerque",
			Province:  "New Mexico",
			Zip:       "87109",
			Country:   "United States",
		},
	})
	if err != nil {
		t.Fatalf("InvoiceSettlementsWithShipping returned error: %v", err)
	}

	if shop.lastDraftInput.ShippingLine != nil {
		t.Fatal("expected settlement invoice to omit shippingLine")
	}

	if shop.lastDraftInput.BillingAddress == nil || shop.lastDraftInput.ShippingAddress == nil {
		t.Fatal("expected shipping and billing addresses on draft order")
	}

	if len(shop.lastDraftInput.LineItems) != 2 {
		t.Fatalf("expected 2 line items (balance + shipping), got %d", len(shop.lastDraftInput.LineItems))
	}

	shippingItem := shop.lastDraftInput.LineItems[1]
	if shippingItem.Title != "UPS Ground" {
		t.Fatalf("unexpected shipping title: %s", shippingItem.Title)
	}
	if shippingItem.OriginalUnitPrice != "17.50" {
		t.Fatalf("unexpected shipping price: %s", shippingItem.OriginalUnitPrice)
	}
	if len(shippingItem.CustomAttributes) == 0 || shippingItem.CustomAttributes[0].Value != "pre_order_shipping" {
		t.Fatalf("expected pre_order_shipping charge_type attribute, got %+v", shippingItem.CustomAttributes)
	}
}
