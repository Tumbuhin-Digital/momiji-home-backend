package checkout

import (
	"context"
	"errors"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
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
func (s *stubProductService) UpdateVariantLtl(context.Context, string, bool) (*product.VariantDTO, error) {
	return nil, nil
}
func (s *stubProductService) LinkCustomVariantSKU(context.Context, string, string) (*product.VariantDTO, error) {
	return nil, nil
}
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
func (s *stubProductService) CreateCustomProduct(context.Context, product.CreateCustomProductInput) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *stubProductService) AddProductVariants(context.Context, product.AddProductVariantsInput) (*product.ProductDTO, error) {
	return nil, nil
}
func (s *stubProductService) SyncInventoryQuantities(context.Context, map[string]int) error {
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
func (s *stubCartService) SetVariantQuantity(context.Context, *string, *string, string, int, bool, bool) (*cart.SetVariantQuantityResponse, error) {
	return &cart.SetVariantQuantityResponse{}, nil
}
func (s *stubCartService) ReconcileShipReadyAgainstInventory(context.Context, *string, *string, map[string]int) (*cart.ReconcileShipReadyResult, error) {
	return &cart.ReconcileShipReadyResult{}, nil
}

type recordingManualShopClient struct {
	lastDraft       shopify.DraftOrderInput
	lastUpdatedID   string
	clearedShipping bool
	draftDetail     *shopify.DraftOrderDetail
	draftDetailErr  error
	sentInvoiceID   string
	sentEmailTo     string
	sendInvoiceErr  error
	createErr       error
	updateErr       error
}

func (c *recordingManualShopClient) QueryAdminGraphQL(context.Context, string, map[string]interface{}) ([]byte, error) {
	return nil, nil
}
func (c *recordingManualShopClient) ListOpenDraftOrders(context.Context) ([]shopify.OpenDraftOrder, error) {
	return nil, nil
}
func (c *recordingManualShopClient) GetDraftOrder(context.Context, string) (*shopify.DraftOrderDetail, error) {
	if c.draftDetail != nil {
		return c.draftDetail, c.draftDetailErr
	}
	return &shopify.DraftOrderDetail{
		ID:     "gid://shopify/DraftOrder/99",
		Status: "OPEN",
		Email:  "jane@example.com",
		CustomAttributes: []shopify.AttributeInput{
			shopify.WholesaleSourceAttribute,
		},
		ShippingAddress: &shopify.DraftOrderAddress{
			FirstName: "Jane",
			LastName:  "Doe",
			Address1:  "1 Main St",
			City:      "Passaic",
			Province:  "NJ",
			Zip:       "07055",
			Country:   "US",
			Phone:     "+15551234567",
		},
	}, nil
}
func (c *recordingManualShopClient) UpdateDraftOrder(_ context.Context, id string, input shopify.DraftOrderInput, clearShippingLine bool) (*shopify.DraftOrderResponse, error) {
	c.lastUpdatedID = id
	c.lastDraft = input
	c.clearedShipping = clearShippingLine
	if c.updateErr != nil {
		return nil, c.updateErr
	}
	return &shopify.DraftOrderResponse{ID: id, InvoiceUrl: "https://example.com/invoice/99"}, nil
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
func (c *recordingManualShopClient) DeleteDraftOrder(context.Context, string) error {
	return nil
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
func (c *recordingManualShopClient) CreateUnlistedProduct(context.Context, shopify.CreateUnlistedProductInput) (*shopify.CreatedProduct, error) {
	return nil, nil
}
func (c *recordingManualShopClient) AddProductVariants(context.Context, shopify.AddProductVariantsInput) ([]shopify.CreatedVariant, error) {
	return nil, nil
}
func (c *recordingManualShopClient) ListProductVariantIDs(context.Context, string) ([]string, error) {
	return nil, nil
}
func (c *recordingManualShopClient) AttachProductMediaFromURL(context.Context, string, string, string) (*shopify.CreatedProductMedia, error) {
	return nil, nil
}
func (c *recordingManualShopClient) AttachProductMediaFromBytes(context.Context, string, string, string, []byte, string) (*shopify.CreatedProductMedia, error) {
	return nil, nil
}
func (c *recordingManualShopClient) LinkVariantSKU(context.Context, string, string) error {
	return nil
}

type stubWarehouseResolver struct{}

func (stubWarehouseResolver) ResolveOrigin(_, requested string) string {
	if requested != "" {
		return requested
	}
	return warehouse.CodeEast
}

func (stubWarehouseResolver) GetOrigin(context.Context, string) (warehouse.Origin, error) {
	return warehouse.Origin{
		Code:              warehouse.CodeEast,
		Name:              "Momiji Home",
		Phone:             "555-0100",
		Address1:          "100 Momiji Way",
		City:              "Passaic",
		State:             "NJ",
		Zip:               "07055",
		Country:           "US",
		GroundServiceCode: "ups_ground",
	}, nil
}

type stubRateClient struct {
	err error
}

func (s *stubRateClient) GetRates(context.Context, shipstation.RateRequest) ([]shipstation.Rate, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []shipstation.Rate{{
		ServiceCode:    "ups_ground",
		ShippingAmount: shipstation.Money{Currency: "USD", Amount: 12},
	}}, nil
}

func (s *stubRateClient) ListCarriers(context.Context) ([]shipstation.Carrier, error) {
	return nil, nil
}

func (s *stubRateClient) TrackShipment(context.Context, string, string) (*shipstation.TrackingResponse, error) {
	return nil, nil
}

func testZipStore() *zipLookupStore {
	return &zipLookupStore{byZip: map[string]*UsZipCode{
		"10001": {ZipCode: "10001", City: "New York", StateAbbr: "NY", StateName: "New York"},
		"07055": {ZipCode: "07055", City: "Passaic", StateAbbr: "NJ", StateName: "New Jersey"},
	}}
}

func newManualOrderService(products *stubProductService, shop *recordingManualShopClient) *service {
	return &service{
		cartService:       &stubCartService{},
		productService:    products,
		shopifyCli:        shop,
		store:             testZipStore(),
		warehouseResolver: stubWarehouseResolver{},
		shipstationCli:    &stubRateClient{},
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

func TestCreateManualOrder_DoesNotSendInvoice(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/1": {
			ID:                "gid://v/1",
			Title:             "Shelf",
			WSPrice:           "180.00",
			RetailPrice:       "300.00",
			FulfillmentType:   product.FulfillmentTypeShipReady,
			InventoryQuantity: 10,
			WeightKg:          4,
		},
		"gid://v/2": {
			ID:                "gid://v/2",
			Title:             "Pre Shelf",
			WSPrice:           "200.00",
			RetailPrice:       "400.00",
			FulfillmentType:   product.FulfillmentTypePreOrder,
			InventoryQuantity: 0,
			WeightKg:          3,
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
	if res.InvoiceEmailSent {
		t.Fatal("expected invoice email not sent on create")
	}
	if shop.sentInvoiceID != "" || shop.sentEmailTo != "" {
		t.Fatalf("expected no invoice email on create, got id=%q to=%q", shop.sentInvoiceID, shop.sentEmailTo)
	}
	if shop.lastDraft.Email != "jane@example.com" {
		t.Fatalf("expected draft email set, got %s", shop.lastDraft.Email)
	}
	if len(shop.lastDraft.LineItems) != 3 {
		t.Fatalf("expected 2 product lines plus shipping deposit, got %d", len(shop.lastDraft.LineItems))
	}
}

func TestCreateManualOrder_ShipTogetherTreatsAllAsPreorder(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/1": {
			ID:                "gid://v/1",
			Title:             "Shelf",
			WSPrice:           "180.00",
			RetailPrice:       "300.00",
			FulfillmentType:   product.FulfillmentTypeShipReady,
			InventoryQuantity: 10,
			WeightKg:          4,
		},
		"gid://v/2": {
			ID:                "gid://v/2",
			Title:             "Pre Shelf",
			WSPrice:           "200.00",
			RetailPrice:       "400.00",
			FulfillmentType:   product.FulfillmentTypePreOrder,
			InventoryQuantity: 0,
			WeightKg:          3,
		},
	}}
	shop := &recordingManualShopClient{}
	svc := newManualOrderService(products, shop)

	_, err := svc.CreateManualOrder(context.Background(), ManualOrderRequest{
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
		ShipTogether:   true,
		LineItems: []ManualOrderLineItem{
			{VariantID: "gid://v/1", Quantity: 1},
			{VariantID: "gid://v/2", Quantity: 1},
		},
	})
	if err != nil {
		t.Fatalf("CreateManualOrder error: %v", err)
	}
	if len(shop.lastDraft.LineItems) != 3 {
		t.Fatalf("expected 2 product lines plus shipping deposit, got %d", len(shop.lastDraft.LineItems))
	}
	productLines := 0
	hasDeposit := false
	for i, line := range shop.lastDraft.LineItems {
		if line.VariantID != "" {
			t.Fatalf("line %d should not use Shopify variant (no stock touch), got %q", i, line.VariantID)
		}
		for _, attr := range line.CustomAttributes {
			if attr.Key == "type" && attr.Value == "preorder_dp" {
				productLines++
			}
			if attr.Key == "charge_type" && attr.Value == "pre_order_shipping_deposit" {
				hasDeposit = true
			}
		}
	}
	if productLines != 2 || !hasDeposit {
		t.Fatalf("expected 2 preorder lines and a shipping deposit, products=%d deposit=%v", productLines, hasDeposit)
	}
	if shop.lastDraft.ShippingLine != nil {
		t.Fatal("expected no ship-ready shipping line when ship_together")
	}
	foundShipTogether := false
	for _, attr := range shop.lastDraft.CustomAttributes {
		if attr.Key == "ship_together" && attr.Value == "true" {
			foundShipTogether = true
		}
	}
	if !foundShipTogether {
		t.Fatal("expected ship_together note attribute")
	}
}

func TestSendManualOrderInvoice_SendsToRequestedEmail(t *testing.T) {
	shop := &recordingManualShopClient{}
	svc := newManualOrderService(&stubProductService{}, shop)

	res, err := svc.SendManualOrderInvoice(context.Background(), SendManualOrderInvoiceRequest{
		DraftOrderID: "gid://shopify/DraftOrder/99",
		Email:        "jane@example.com",
	})
	if err != nil {
		t.Fatalf("SendManualOrderInvoice error: %v", err)
	}
	if !res.InvoiceEmailSent {
		t.Fatal("expected invoice_email_sent true")
	}
	if shop.sentInvoiceID != "gid://shopify/DraftOrder/99" {
		t.Fatalf("expected send on draft id, got %s", shop.sentInvoiceID)
	}
	if shop.sentEmailTo != "jane@example.com" {
		t.Fatalf("expected email to jane, got %s", shop.sentEmailTo)
	}
}

func TestSendManualOrderInvoice_SendFailure(t *testing.T) {
	shop := &recordingManualShopClient{sendInvoiceErr: errors.New("smtp down")}
	svc := newManualOrderService(&stubProductService{}, shop)

	res, err := svc.SendManualOrderInvoice(context.Background(), SendManualOrderInvoiceRequest{
		DraftOrderID: "gid://shopify/DraftOrder/99",
		Email:        "jane@example.com",
	})
	if err == nil {
		t.Fatal("expected send failure")
	}
	if res != nil {
		t.Fatalf("expected nil response, got %+v", res)
	}
	if shop.sentInvoiceID != "gid://shopify/DraftOrder/99" {
		t.Fatalf("expected send attempted, got %s", shop.sentInvoiceID)
	}
}

func TestSendManualOrderInvoice_MissingFields(t *testing.T) {
	svc := newManualOrderService(&stubProductService{}, &recordingManualShopClient{})
	_, err := svc.SendManualOrderInvoice(context.Background(), SendManualOrderInvoiceRequest{
		DraftOrderID: "  ",
		Email:        "jane@example.com",
	})
	if err == nil {
		t.Fatal("expected error for blank draft order id")
	}
}

func TestSendManualOrderInvoice_RejectsCompleted(t *testing.T) {
	shop := &recordingManualShopClient{
		draftDetail: &shopify.DraftOrderDetail{
			ID:     "gid://shopify/DraftOrder/99",
			Status: "COMPLETED",
			CustomAttributes: []shopify.AttributeInput{
				shopify.WholesaleSourceAttribute,
			},
		},
	}
	svc := newManualOrderService(&stubProductService{}, shop)
	_, err := svc.SendManualOrderInvoice(context.Background(), SendManualOrderInvoiceRequest{
		DraftOrderID: "gid://shopify/DraftOrder/99",
		Email:        "jane@example.com",
	})
	if err == nil {
		t.Fatal("expected completed draft to be rejected")
	}
}

func TestUpdateDraftOrderItems_ReplacesProductLines(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/1": {
			ID:                "gid://v/1",
			Title:             "Shelf",
			WSPrice:           "40.00",
			RetailPrice:       "80.00",
			FulfillmentType:   product.FulfillmentTypeShipReady,
			InventoryQuantity: 5,
			WeightKg:          2,
		},
	}}
	shop := &recordingManualShopClient{}
	svc := newManualOrderService(products, shop)

	res, err := svc.UpdateDraftOrderItems(context.Background(), UpdateDraftOrderItemsRequest{
		DraftOrderID:   "gid://shopify/DraftOrder/99",
		ShippingMethod: "ups_ground",
		LineItems: []ManualOrderLineItem{
			{VariantID: "gid://v/1", Quantity: 2},
		},
	})
	if err != nil {
		t.Fatalf("UpdateDraftOrderItems error: %v", err)
	}
	if res.DraftOrderID != "gid://shopify/DraftOrder/99" {
		t.Fatalf("unexpected draft id %s", res.DraftOrderID)
	}
	if len(shop.lastDraft.LineItems) != 1 || shop.lastDraft.LineItems[0].Quantity != 2 {
		t.Fatalf("expected one rebuilt line, got %+v", shop.lastDraft.LineItems)
	}
	if shop.lastDraft.Email != "jane@example.com" {
		t.Fatalf("expected draft email to be kept, got %s", shop.lastDraft.Email)
	}
}

func TestCreateManualOrder_RateFailureDoesNotCreateDraft(t *testing.T) {
	products := &stubProductService{variants: map[string]*product.VariantDTO{
		"gid://v/1": {
			ID:                "gid://v/1",
			Title:             "Shelf",
			WSPrice:           "180.00",
			RetailPrice:       "300.00",
			FulfillmentType:   product.FulfillmentTypeShipReady,
			InventoryQuantity: 10,
			WeightKg:          4,
		},
	}}
	shop := &recordingManualShopClient{}
	svc := newManualOrderService(products, shop)
	svc.shipstationCli = &stubRateClient{err: &shipstation.APIError{
		StatusCode: 400,
		Message:    "Invalid postal code for the shipping address",
	}}

	_, err := svc.CreateManualOrder(context.Background(), ManualOrderRequest{
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
		LineItems: []ManualOrderLineItem{
			{VariantID: "gid://v/1", Quantity: 1},
		},
	})
	var appErr *apierror.AppError
	if !errors.As(err, &appErr) || appErr.Code != shipping.CodeAddressRejected {
		t.Fatalf("expected address_rejected, got %v", err)
	}
	if shop.lastDraft.Email != "" {
		t.Fatalf("draft was created despite rate failure: %+v", shop.lastDraft)
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

func TestBuildPreOrderShippingDepositLine(t *testing.T) {
	line := buildPreOrderShippingDepositLine(228.32)

	if line.Title != PreOrderShippingDepositTitle {
		t.Fatalf("expected title %q, got %q", PreOrderShippingDepositTitle, line.Title)
	}
	if line.OriginalUnitPrice != "228.32" {
		t.Fatalf("expected price 228.32, got %q", line.OriginalUnitPrice)
	}
	if line.Quantity != 1 {
		t.Fatalf("expected quantity 1, got %d", line.Quantity)
	}
	if line.RequiresShipping == nil || *line.RequiresShipping {
		t.Fatal("shipping deposit must not itself require shipping")
	}
	// The paid-order webhook keys off this attribute to keep the charge out of order items.
	var chargeType string
	for _, attr := range line.CustomAttributes {
		if attr.Key == "charge_type" {
			chargeType = attr.Value
		}
	}
	if chargeType != "pre_order_shipping_deposit" {
		t.Fatalf("expected charge_type pre_order_shipping_deposit, got %q", chargeType)
	}
}
