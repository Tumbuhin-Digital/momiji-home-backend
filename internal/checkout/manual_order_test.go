package checkout

import (
	"context"
	"errors"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type stubProductService struct {
	variants map[string]*product.VariantDTO
}

func (s *stubProductService) GetVariantByID(_ context.Context, variantID string) (*product.VariantDTO, error) {
	if v, ok := s.variants[variantID]; ok {
		return v, nil
	}
	return nil, apierror.ErrNotFound
}

func (s *stubProductService) GetProducts(context.Context, product.ProductQuery) ([]product.ProductDTO, int64, error) {
	return nil, 0, nil
}
func (s *stubProductService) SyncFromShopify(context.Context) error { return nil }
func (s *stubProductService) GetProductByID(context.Context, string) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *stubProductService) GetVariantsByProductID(context.Context, string) ([]product.VariantDTO, error) {
	return nil, nil
}
func (s *stubProductService) UpdateProductStatus(context.Context, string, string) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *stubProductService) UpdateVariantStatus(context.Context, string, string) (*product.VariantDTO, error) {
	return nil, nil
}
func (s *stubProductService) UpdateVariantBatchLabel(context.Context, string, string, *string) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *stubProductService) UpdateVariantBatchLabelByVariantID(context.Context, string, string) (*product.VariantDTO, error) {
	return nil, nil
}
func (s *stubProductService) UpdateVariantPrice(context.Context, string, *float64) error { return nil }
func (s *stubProductService) GetAllVariants(context.Context) ([]product.ProductVariant, error) {
	return nil, nil
}
func (s *stubProductService) BulkUpdateDimensions(context.Context, []product.DimensionUpdateInput) (product.BulkUpdateDimensionsResult, error) {
	return product.BulkUpdateDimensionsResult{}, nil
}
func (s *stubProductService) ValidateVariantActive(_ context.Context, variantID string) error {
	v, ok := s.variants[variantID]
	if !ok {
		return apierror.ErrNotFound
	}
	if v.FulfillmentType == product.FulfillmentTypeInactive {
		return apierror.New(422, "inactive_variant", "inactive")
	}
	return nil
}

type stubCartService struct{}

func (s *stubCartService) CreateGuestSession(context.Context) (*cart.GuestSessionResponse, error) {
	return nil, nil
}
func (s *stubCartService) GetCartResponse(context.Context, *string, *string) (*cart.CartResponse, error) {
	return &cart.CartResponse{}, nil
}
func (s *stubCartService) GetCartSummary(context.Context, *string, *string) (*cart.CartSummaryDTO, error) {
	return nil, nil
}
func (s *stubCartService) AddItem(context.Context, *string, *string, cart.CartItemRequest) error {
	return nil
}
func (s *stubCartService) UpdateItemQuantity(context.Context, *string, *string, string, cart.UpdateCartItemRequest) error {
	return nil
}
func (s *stubCartService) RemoveItem(context.Context, *string, *string, string) error { return nil }
func (s *stubCartService) ClearCart(context.Context, *string, *string) error          { return nil }
func (s *stubCartService) MergeCarts(context.Context, string, string) error           { return nil }
func (s *stubCartService) SetVariantQuantity(context.Context, *string, *string, string, int) error {
	return nil
}

type recordingManualShopClient struct {
	lastDraft      shopify.DraftOrderInput
	sentInvoiceID  string
	sentEmailTo    string
	sendInvoiceErr error
	createErr      error
}

func (c *recordingManualShopClient) QueryAdminGraphQL(context.Context, string, map[string]interface{}) ([]byte, error) {
	return nil, nil
}
func (c *recordingManualShopClient) CreateDraftOrder(_ context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	if c.createErr != nil {
		return nil, c.createErr
	}
	c.lastDraft = input
	return &shopify.DraftOrderResponse{
		ID:         "gid://shopify/DraftOrder/99",
		InvoiceUrl: "https://example.com/invoice/99",
	}, nil
}
func (c *recordingManualShopClient) SendDraftOrderInvoice(_ context.Context, draftOrderID string, email *shopify.DraftOrderInvoiceEmailInput) error {
	c.sentInvoiceID = draftOrderID
	if email != nil {
		c.sentEmailTo = email.To
	}
	return c.sendInvoiceErr
}
func (c *recordingManualShopClient) CreateStorefrontCart(context.Context, shopify.CartCreateInput) (*shopify.CartCreateResponse, error) {
	return nil, nil
}
func (c *recordingManualShopClient) CreateRefund(context.Context, string, float64, string, string) error {
	return nil
}
func (c *recordingManualShopClient) GetVariantsInventory(context.Context, []string) (map[string]int, error) {
	return nil, nil
}
func (c *recordingManualShopClient) CreateFulfillment(context.Context, string) error { return nil }
func (c *recordingManualShopClient) FetchFulfillmentOrders(context.Context, string) ([]shopify.FulfillmentOrderData, error) {
	return nil, nil
}
func (c *recordingManualShopClient) CreateFulfillmentV2(context.Context, shopify.CreateFulfillmentV2Input) (*shopify.CreateFulfillmentV2Result, error) {
	return nil, nil
}
func (c *recordingManualShopClient) CreateFulfillmentEvent(context.Context, string, string) error {
	return nil
}

func newManualOrderService(products *stubProductService, shop *recordingManualShopClient) *service {
	return &service{
		cartService:    &stubCartService{},
		productService: products,
		shopifyCli:     shop,
		store:          &zipLookupStore{},
	}
}

func TestSplitManualLineItems_InventoryOverflow(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/1": {
			ID:                "gid://v/1",
			Title:             "Shelf",
			WSPrice:           "100.00",
			RetailPrice:       "200.00",
			FulfillmentType:   product.FulfillmentTypeShipReady,
			InventoryQuantity: 1,
			WeightKg:          10,
		},
	}}
	svc := newManualOrderService(products, &recordingManualShopClient{})

	ship, pre, err := svc.splitManualLineItems(context.Background(), []ManualOrderLineItem{
		{VariantID: "gid://v/1", Quantity: 3},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ship) != 1 || ship[0].Quantity != 1 {
		t.Fatalf("expected 1 ship_ready qty 1, got %+v", ship)
	}
	if len(pre) != 1 || pre[0].Quantity != 2 {
		t.Fatalf("expected 1 pre_order qty 2, got %+v", pre)
	}
}

func TestResolveExplicitLineItems(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/2": {
			ID:          "gid://v/2",
			Title:       "Table",
			WSPrice:     "50.00",
			RetailPrice: "80.00",
			WeightKg:    15,
			LengthCm:    65,
			WidthCm:     80,
			HeightCm:    70,
		},
	}}
	svc := newManualOrderService(products, &recordingManualShopClient{})

	items, err := svc.resolveExplicitLineItems(context.Background(), []ShippingRateLineItem{
		{VariantID: "gid://v/2", Quantity: 2},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 || items[0].Quantity != 2 || items[0].Weight != 15 {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestCreateManualOrder_SendsInvoice(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/1": {
			ID:                "gid://v/1",
			Title:             "Shelf",
			WSPrice:           "180.00",
			RetailPrice:       "300.00",
			FulfillmentType:   product.FulfillmentTypeShipReady,
			InventoryQuantity: 10,
		},
		"gid://v/2": {
			ID:                "gid://v/2",
			Title:             "Pre Shelf",
			WSPrice:           "200.00",
			RetailPrice:       "400.00",
			FulfillmentType:   product.FulfillmentTypePreOrder,
			InventoryQuantity: 0,
		},
	}}
	shop := &recordingManualShopClient{}
	svc := newManualOrderService(products, shop)

	res, err := svc.CreateManualOrder(context.Background(), ManualOrderRequest{
		Email:          "jane@example.com",
		FirstName:      "Jane",
		LastName:       "Doe",
		Phone:          "+15551234567",
		Address1:       "1 Main St",
		City:           "New York",
		State:          "NY",
		Zip:            "10001",
		Country:        "US",
		ShippingMethod: "ups_ground",
		Origin:         "west",
		LineItems: []ManualOrderLineItem{
			{VariantID: "gid://v/1", Quantity: 1},
			{VariantID: "gid://v/2", Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("CreateManualOrder error: %v", err)
	}
	if res.InvoiceURL != "https://example.com/invoice/99" {
		t.Fatalf("unexpected invoice url: %s", res.InvoiceURL)
	}
	if !res.InvoiceEmailSent {
		t.Fatal("expected invoice email sent")
	}
	if shop.sentInvoiceID != "gid://shopify/DraftOrder/99" {
		t.Fatalf("expected send on draft id, got %s", shop.sentInvoiceID)
	}
	if shop.sentEmailTo != "jane@example.com" {
		t.Fatalf("expected email to jane, got %s", shop.sentEmailTo)
	}
	if shop.lastDraft.Email != "jane@example.com" {
		t.Fatalf("expected draft email set, got %s", shop.lastDraft.Email)
	}
	if len(shop.lastDraft.LineItems) != 2 {
		t.Fatalf("expected 2 draft lines, got %d", len(shop.lastDraft.LineItems))
	}
}

func TestCreateManualOrder_SendFailureStillReturnsURL(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/1": {
			ID:                "gid://v/1",
			Title:             "Shelf",
			WSPrice:           "180.00",
			RetailPrice:       "300.00",
			FulfillmentType:   product.FulfillmentTypeShipReady,
			InventoryQuantity: 5,
		},
	}}
	shop := &recordingManualShopClient{sendInvoiceErr: errors.New("smtp down")}
	svc := newManualOrderService(products, shop)

	res, err := svc.CreateManualOrder(context.Background(), ManualOrderRequest{
		Email:     "jane@example.com",
		FirstName: "Jane",
		LastName:  "Doe",
		Phone:     "+15551234567",
		Address1:  "1 Main St",
		City:      "New York",
		State:     "NY",
		Zip:       "10001",
		Country:   "US",
		LineItems: []ManualOrderLineItem{{VariantID: "gid://v/1", Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("expected success with email failure, got %v", err)
	}
	if res.InvoiceEmailSent {
		t.Fatal("expected invoice_email_sent false")
	}
	if res.InvoiceURL == "" {
		t.Fatal("expected invoice url even when email fails")
	}
}

func TestCreateManualOrder_EmptyLineItems(t *testing.T) {
	svc := newManualOrderService(&stubProductService{variants: map[string]*product.VariantDTO{}}, &recordingManualShopClient{})
	_, err := svc.CreateManualOrder(context.Background(), ManualOrderRequest{
		Email:     "a@b.com",
		FirstName: "A",
		LastName:  "B",
		Phone:     "+15551234567",
		Address1:  "1 Main",
		City:      "NYC",
		State:     "NY",
		Zip:       "10001",
		Country:   "US",
	})
	if err == nil {
		t.Fatal("expected error for empty line items")
	}
}

func TestCreateManualOrder_PreOrderRequiresShippingMethod(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/2": {
			ID:              "gid://v/2",
			Title:           "Pre",
			WSPrice:         "100.00",
			RetailPrice:     "200.00",
			FulfillmentType: product.FulfillmentTypePreOrder,
		},
	}}
	svc := newManualOrderService(products, &recordingManualShopClient{})
	_, err := svc.CreateManualOrder(context.Background(), ManualOrderRequest{
		Email:     "a@b.com",
		FirstName: "A",
		LastName:  "B",
		Phone:     "+15551234567",
		Address1:  "1 Main",
		City:      "NYC",
		State:     "NY",
		Zip:       "10001",
		Country:   "US",
		LineItems: []ManualOrderLineItem{{VariantID: "gid://v/2", Quantity: 1}},
	})
	if err == nil {
		t.Fatal("expected shipping_method required error")
	}
}

func TestBuildDraftLinesFromSegments(t *testing.T) {
	lines := buildDraftLinesFromSegments(
		[]cart.CartItem{{VariantID: "v1", Quantity: 1, UnitPrice: "10.00", Title: "A"}},
		[]cart.CartItem{{VariantID: "v2", Quantity: 2, UnitPrice: "20.00", Title: "B"}},
	)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].VariantID != "v1" {
		t.Fatalf("expected ship ready variant, got %s", lines[0].VariantID)
	}
	if lines[1].Title == "" || lines[1].OriginalUnitPrice != "10.00" {
		t.Fatalf("expected preorder deposit line, got %+v", lines[1])
	}
}
