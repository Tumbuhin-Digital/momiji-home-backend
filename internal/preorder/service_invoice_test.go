package preorder

import (
	"context"
	"errors"
	"sync"
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

type failingShopClient struct{}

func (c *failingShopClient) QueryAdminGraphQL(context.Context, string, map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (c *failingShopClient) CreateDraftOrder(context.Context, shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return nil, errors.New("shopify unavailable")
}

func (c *failingShopClient) CreateStorefrontCart(context.Context, shopify.CartCreateInput) (*shopify.CartCreateResponse, error) {
	return nil, nil
}

func (c *failingShopClient) CreateRefund(context.Context, string, float64, string, string) error {
	return nil
}

func (c *failingShopClient) GetVariantsInventory(context.Context, []string) (map[string]int, error) {
	return nil, nil
}

func (c *failingShopClient) CreateFulfillment(context.Context, string) error { return nil }

func (c *failingShopClient) FetchFulfillmentOrders(context.Context, string) ([]shopify.FulfillmentOrderData, error) {
	return nil, nil
}

func (c *failingShopClient) CreateFulfillmentV2(context.Context, shopify.CreateFulfillmentV2Input) (*shopify.CreateFulfillmentV2Result, error) {
	return nil, nil
}

func (c *failingShopClient) CreateFulfillmentEvent(context.Context, string, string) error { return nil }

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
	settlements  map[string]Settlement
	reminderRows map[int][]PreorderRow
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

func (s *invoiceTestStore) MarkSettlementsInvoiced(_ context.Context, ids []string, draftOrderID, invoiceURL string, invoicedAt time.Time) error {
	for _, id := range ids {
		st, ok := s.settlements[id]
		if !ok || st.Status != "pending" {
			return errors.New("settlement not pending")
		}
		st.Status = "invoiced"
		st.InvoicedAt = &invoicedAt
		st.ShopifyDraftOrderID = &draftOrderID
		st.ShopifyInvoiceURL = &invoiceURL
		s.settlements[id] = st
	}
	return nil
}

func (s *invoiceTestStore) AllSettlementsPaid(context.Context, string) (bool, error) { return false, nil }

func (s *invoiceTestStore) GetSettlementsForReminder(_ context.Context, daysSinceInvoiced int) ([]PreorderRow, error) {
	if s.reminderRows == nil {
		return nil, nil
	}
	return s.reminderRows[daysSinceInvoiced], nil
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

type trackingEmailService struct {
	noopEmailService
	mu            sync.Mutex
	invoiceSent   bool
	invoiceLink   string
	reminderLinks []string
}

func (s *trackingEmailService) SendInvoice(_ context.Context, _ string, data email.SettlementEmailData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoiceSent = true
	s.invoiceLink = data.PaymentLink
	return nil
}

func (s *trackingEmailService) SendReminder(_ context.Context, _ string, data email.SettlementEmailData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reminderLinks = append(s.reminderLinks, data.PaymentLink)
	return nil
}

func pendingSettlement() Settlement {
	return Settlement{
		ID:              "settle-1",
		OrderLineItemID: "line-1",
		OrderID:         "order-1",
		Title:           "test item",
		BalanceAmount:   50,
		Status:          "pending",
		CustomerEmail:   "buyer@example.com",
		CustomerName:    "Buyer",
	}
}

func TestInvoiceSettlementsWithShippingAddsShippingLineItem(t *testing.T) {
	store := &invoiceTestStore{
		settlements: map[string]Settlement{
			"settle-1": pendingSettlement(),
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

	st := store.settlements["settle-1"]
	if st.Status != "invoiced" {
		t.Fatalf("expected settlement invoiced, got %s", st.Status)
	}
	if st.ShopifyInvoiceURL == nil || *st.ShopifyInvoiceURL != "https://example.com/invoice" {
		t.Fatalf("expected shopify invoice URL persisted, got %+v", st.ShopifyInvoiceURL)
	}
	if st.ShopifyDraftOrderID == nil || *st.ShopifyDraftOrderID != "gid://shopify/DraftOrder/1" {
		t.Fatalf("expected shopify draft order ID persisted, got %+v", st.ShopifyDraftOrderID)
	}
}

func TestInvoiceSettlementsWithShipping_DraftOrderFailure_KeepsPending(t *testing.T) {
	store := &invoiceTestStore{
		settlements: map[string]Settlement{
			"settle-1": pendingSettlement(),
		},
	}
	emailSvc := &trackingEmailService{}

	svc := &service{
		store:        store,
		shopClient:   &failingShopClient{},
		emailService: emailSvc,
	}

	_, err := svc.InvoiceSettlementsWithShipping(context.Background(), []string{"line-1"}, InvoiceOptions{})
	if err == nil {
		t.Fatal("expected error when draft order creation fails")
	}

	st := store.settlements["settle-1"]
	if st.Status != "pending" {
		t.Fatalf("expected settlement to remain pending, got %s", st.Status)
	}
	if st.InvoicedAt != nil {
		t.Fatal("expected invoiced_at to remain unset")
	}
	if st.ShopifyInvoiceURL != nil {
		t.Fatal("expected shopify invoice URL to remain unset")
	}

	emailSvc.mu.Lock()
	defer emailSvc.mu.Unlock()
	if emailSvc.invoiceSent {
		t.Fatal("expected invoice email not to be sent when draft order fails")
	}
}

func TestInvoiceSettlementsWithShipping_PersistsInvoiceURLAndSendsEmail(t *testing.T) {
	store := &invoiceTestStore{
		settlements: map[string]Settlement{
			"settle-1": pendingSettlement(),
		},
	}
	emailSvc := &trackingEmailService{}

	svc := &service{
		store:        store,
		shopClient:   &recordingShopClient{},
		emailService: emailSvc,
	}

	res, err := svc.InvoiceSettlementsWithShipping(context.Background(), []string{"line-1"}, InvoiceOptions{})
	if err != nil {
		t.Fatalf("InvoiceSettlementsWithShipping returned error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 response, got %d", len(res))
	}
	if res[0].ShopifyInvoiceURL == nil || *res[0].ShopifyInvoiceURL != "https://example.com/invoice" {
		t.Fatalf("expected response to include shopify invoice URL, got %+v", res[0].ShopifyInvoiceURL)
	}

	time.Sleep(10 * time.Millisecond)

	emailSvc.mu.Lock()
	defer emailSvc.mu.Unlock()
	if !emailSvc.invoiceSent {
		t.Fatal("expected invoice email to be sent")
	}
	if emailSvc.invoiceLink != "https://example.com/invoice" {
		t.Fatalf("expected invoice email payment link %q, got %q", "https://example.com/invoice", emailSvc.invoiceLink)
	}
}

func TestProcessReminders_UsesStoredInvoiceURL(t *testing.T) {
	invoiceURL := "https://example.com/invoice"
	store := &invoiceTestStore{
		reminderRows: map[int][]PreorderRow{
			3: {{
				ID:                "settle-1",
				CustomerEmail:     "buyer@example.com",
				CustomerName:      "Buyer",
				Title:             "test item",
				BalanceAmount:     50,
				ShopifyInvoiceURL: &invoiceURL,
			}},
		},
	}
	emailSvc := &trackingEmailService{}

	svc := &service{
		store:        store,
		emailService: emailSvc,
	}

	if err := svc.ProcessReminders(context.Background()); err != nil {
		t.Fatalf("ProcessReminders returned error: %v", err)
	}

	emailSvc.mu.Lock()
	defer emailSvc.mu.Unlock()
	if len(emailSvc.reminderLinks) != 1 {
		t.Fatalf("expected 1 reminder email, got %d", len(emailSvc.reminderLinks))
	}
	if emailSvc.reminderLinks[0] != invoiceURL {
		t.Fatalf("expected reminder payment link %q, got %q", invoiceURL, emailSvc.reminderLinks[0])
	}
}

func TestProcessReminders_SkipsWhenInvoiceURLMissing(t *testing.T) {
	store := &invoiceTestStore{
		reminderRows: map[int][]PreorderRow{
			3: {{
				ID:            "settle-1",
				CustomerEmail: "buyer@example.com",
				CustomerName:  "Buyer",
				Title:         "test item",
				BalanceAmount: 50,
			}},
		},
	}
	emailSvc := &trackingEmailService{}

	svc := &service{
		store:        store,
		emailService: emailSvc,
	}

	if err := svc.ProcessReminders(context.Background()); err != nil {
		t.Fatalf("ProcessReminders returned error: %v", err)
	}

	emailSvc.mu.Lock()
	defer emailSvc.mu.Unlock()
	if len(emailSvc.reminderLinks) != 0 {
		t.Fatalf("expected no reminder emails, got %d", len(emailSvc.reminderLinks))
	}
}
