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

func (c *recordingShopClient) SendDraftOrderInvoice(context.Context, string, *shopify.DraftOrderInvoiceEmailInput) error {
	return nil
}

type failingShopClient struct{}

func (c *failingShopClient) QueryAdminGraphQL(context.Context, string, map[string]interface{}) ([]byte, error) {
	return nil, nil
}

func (c *failingShopClient) CreateDraftOrder(context.Context, shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return nil, errors.New("shopify unavailable")
}

func (c *failingShopClient) SendDraftOrderInvoice(context.Context, string, *shopify.DraftOrderInvoiceEmailInput) error {
	return nil
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
	mu              sync.Mutex
	invoiceSent     bool
	invoiceLink     string
	invoiceHeading  string
	reminderLinks   []string
}

func (s *trackingEmailService) SendInvoice(_ context.Context, _ string, data email.SettlementEmailData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoiceSent = true
	s.invoiceLink = data.PaymentLink
	s.invoiceHeading = data.InvoiceHeading
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
	if shop.lastDraftInput.BillingAddress.Address1 != shop.lastDraftInput.ShippingAddress.Address1 {
		t.Fatal("expected billing to fall back to shipping when billing not provided")
	}

	if len(shop.lastDraftInput.LineItems) != 2 {
		t.Fatalf("expected 2 line items (balance + shipping), got %d", len(shop.lastDraftInput.LineItems))
	}

	shippingItem := shop.lastDraftInput.LineItems[1]
	if shippingItem.Title != "Shipping & delivery (UPS Ground)" {
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

func TestGroupInvoiceHeading(t *testing.T) {
	tests := []struct {
		name      string
		batchName string
		itemCount int
		want      string
	}{
		{
			name:      "batch with items",
			batchName: "Batch - September 2026",
			itemCount: 4,
			want:      "Pre-Order Batch - September 2026 (4 items)",
		},
		{
			name:      "batch without item count",
			batchName: "Batch - September 2026",
			itemCount: 0,
			want:      "Pre-Order Batch - September 2026",
		},
		{
			name:      "unbatched with items",
			batchName: "",
			itemCount: 2,
			want:      "Pre-Order (2 items)",
		},
		{
			name:      "fallback",
			batchName: "",
			itemCount: 0,
			want:      "Pre-order balance invoice",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := groupInvoiceHeading(tt.batchName, tt.itemCount)
			if got != tt.want {
				t.Fatalf("groupInvoiceHeading(%q, %d) = %q, want %q", tt.batchName, tt.itemCount, got, tt.want)
			}
		})
	}
}

func TestCreateGroupSecondPaymentInvoice_IncludesBatchHeadingInEmail(t *testing.T) {
	shop := &recordingShopClient{}
	emailSvc := &trackingEmailService{}
	svc := &service{
		shopClient:   shop,
		emailService: emailSvc,
	}

	_, err := svc.CreateGroupSecondPaymentInvoice(context.Background(), GroupInvoiceOptions{
		CustomerEmail: "buyer@example.com",
		CustomerName:  "Gilang",
		OrderID:       "order-1",
		ShipmentID:    "ship-1",
		BatchName:     "Batch - September 2026",
		ItemCount:     4,
		Lines: []GroupInvoiceLine{{
			Title:           "3-in-1 Grocery Store",
			Amount:          300,
			OrderLineItemID: "line-1",
			Quantity:        2,
		}},
		ShippingTitle: "UPS Ground",
		ShippingPrice: 50,
	})
	if err != nil {
		t.Fatalf("CreateGroupSecondPaymentInvoice returned error: %v", err)
	}

	// Email is sent in a goroutine.
	deadline := time.Now().Add(2 * time.Second)
	for {
		emailSvc.mu.Lock()
		sent := emailSvc.invoiceSent
		heading := emailSvc.invoiceHeading
		emailSvc.mu.Unlock()
		if sent {
			want := "Pre-Order Batch - September 2026 (4 items)"
			if heading != want {
				t.Fatalf("invoice heading = %q, want %q", heading, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for invoice email")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCreateGroupSecondPaymentInvoice_UsesDistinctBillingAddress(t *testing.T) {
	shop := &recordingShopClient{}
	svc := &service{
		shopClient:   shop,
		emailService: noopEmailService{},
	}

	shipping := &shopify.AddressInput{
		FirstName: "Ship",
		LastName:  "To",
		Company:   "Ship Co",
		Address1:  "1 Ship St",
		City:      "Denver",
		Province:  "CO",
		Zip:       "80202",
		Country:   "United States",
		Phone:     "+13035550111",
	}
	billing := &shopify.AddressInput{
		FirstName: "Bill",
		LastName:  "Pay",
		Company:   "Bill Co",
		Address1:  "9 Bill Ave",
		City:      "Austin",
		Province:  "TX",
		Zip:       "78701",
		Country:   "United States",
		Phone:     "+15125550123",
	}

	_, err := svc.CreateGroupSecondPaymentInvoice(context.Background(), GroupInvoiceOptions{
		CustomerEmail: "buyer@example.com",
		CustomerName:  "Buyer",
		OrderID:       "order-1",
		ShipmentID:    "ship-1",
		Lines: []GroupInvoiceLine{{
			Title:           "Item",
			Amount:          100,
			OrderLineItemID: "line-1",
			Quantity:        1,
		}},
		ShippingAddress: shipping,
		BillingAddress:  billing,
	})
	if err != nil {
		t.Fatalf("CreateGroupSecondPaymentInvoice returned error: %v", err)
	}

	if shop.lastDraftInput.ShippingAddress == nil || shop.lastDraftInput.BillingAddress == nil {
		t.Fatal("expected shipping and billing addresses on draft order")
	}
	if shop.lastDraftInput.ShippingAddress.Address1 != "1 Ship St" {
		t.Fatalf("unexpected shipping address: %+v", shop.lastDraftInput.ShippingAddress)
	}
	if shop.lastDraftInput.BillingAddress.Address1 != "9 Bill Ave" {
		t.Fatalf("unexpected billing address: %+v", shop.lastDraftInput.BillingAddress)
	}
	if shop.lastDraftInput.BillingAddress.Company != "Bill Co" {
		t.Fatalf("unexpected billing company: %s", shop.lastDraftInput.BillingAddress.Company)
	}
}

