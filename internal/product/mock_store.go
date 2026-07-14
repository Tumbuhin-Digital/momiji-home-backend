package product

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

// MockProductStore is an in-memory Store for unit tests.
type MockProductStore struct {
	Products     map[string]*Product        // keyed by ShopifyID
	Variants     map[string]*ProductVariant // keyed by ShopifyVariantID
	VariantsByID map[string]*ProductVariant // keyed by UUID ID

	GetProductByShopifyIDCalls int
	UpsertProductCalls         int
	UpsertVariantCalls         int
	UpdateProductStatusCalls   int
	UpdateVariantBatchLabelCalls int

	// Injectable errors
	GetVariantsErr             error
	GetVariantByShopifyIDErr   error
	GetProductByShopifyIDErr   error
	UpsertProductErr           error
	UpsertVariantErr           error
	UpdateVariantPricesErr     error
	GetProductByIDErr          error
	GetVariantsByProductIDErr  error
	UpdateProductStatusErr     error
	UpdateVariantBatchLabelErr error
	UpsertProductImagesErr     error
}

func NewMockProductStore() *MockProductStore {
	return &MockProductStore{
		Products:     make(map[string]*Product),
		Variants:     make(map[string]*ProductVariant),
		VariantsByID: make(map[string]*ProductVariant),
	}
}

func (m *MockProductStore) GetProducts(ctx context.Context, q ProductQuery) ([]Product, int64, error) {
	if m.GetVariantsErr != nil { return nil, 0, m.GetVariantsErr }
	out := make([]Product, 0, len(m.Products))
	for _, p := range m.Products {
		if !strings.EqualFold(p.Status, "active") {
			continue
		}
		out = append(out, *p)
	}
	return out, int64(len(out)), nil
}

func (m *MockProductStore) ListShopifyProductIDs(ctx context.Context) ([]string, error) {
	ids := make([]string, 0, len(m.Products))
	for _, p := range m.Products {
		if strings.EqualFold(p.Status, "deleted") {
			continue
		}
		ids = append(ids, p.ShopifyID)
	}
	return ids, nil
}

func (m *MockProductStore) GetVariantByShopifyID(ctx context.Context, id string) (*ProductVariant, error) {
	if m.GetVariantByShopifyIDErr != nil { return nil, m.GetVariantByShopifyIDErr }
	v, ok := m.Variants[id]
	if !ok { return nil, nil }
	return v, nil
}

func (m *MockProductStore) GetVariantByInventoryItemID(ctx context.Context, id string) (*ProductVariant, error) {
	if m.GetVariantByShopifyIDErr != nil { return nil, m.GetVariantByShopifyIDErr } // reuse err for simplicity
	for _, v := range m.Variants {
		if v.ShopifyInventoryItemID == id {
			return v, nil
		}
	}
	return nil, nil
}

func (m *MockProductStore) GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error) {
	m.GetProductByShopifyIDCalls++
	if m.GetProductByShopifyIDErr != nil { return nil, m.GetProductByShopifyIDErr }
	p, ok := m.Products[shopifyID]
	if !ok { return nil, nil }
	return p, nil
}

func (m *MockProductStore) UpsertProduct(ctx context.Context, product *Product) error {
	m.UpsertProductCalls++
	if m.UpsertProductErr != nil { return m.UpsertProductErr }
	if product.ID == "" { product.ID = "mock-product-uuid" }
	m.Products[product.ShopifyID] = product
	return nil
}

func (m *MockProductStore) MarkProductDeletedByShopifyID(ctx context.Context, shopifyID string) error {
	candidates := []string{shopifyID}
	if _, err := strconv.ParseInt(shopifyID, 10, 64); err == nil {
		candidates = append(candidates, "gid://shopify/Product/"+shopifyID)
	} else if idx := strings.LastIndex(shopifyID, "/"); idx >= 0 && idx < len(shopifyID)-1 {
		candidates = append(candidates, shopifyID[idx+1:])
	}
	for _, id := range candidates {
		if p, ok := m.Products[id]; ok {
			p.Status = "deleted"
		}
	}
	return nil
}

func (m *MockProductStore) UpsertVariant(ctx context.Context, variant *ProductVariant) error {
	m.UpsertVariantCalls++
	if m.UpsertVariantErr != nil { return m.UpsertVariantErr }
	if existing, ok := m.Variants[variant.ShopifyVariantID]; ok {
		// Match Postgres DoUpdates: preserve weight and packaging dims on conflict.
		variant.WeightKg = existing.WeightKg
		variant.WidthCm = existing.WidthCm
		variant.HeightCm = existing.HeightCm
		variant.DepthCm = existing.DepthCm
		if variant.ID == "" {
			variant.ID = existing.ID
		}
		if variant.FulfillmentType == "" {
			variant.FulfillmentType = existing.FulfillmentType
		}
		if variant.PreorderBatchLabel == nil {
			variant.PreorderBatchLabel = existing.PreorderBatchLabel
		}
		if variant.RetailPrice == nil {
			variant.RetailPrice = existing.RetailPrice
		}
		if variant.WSPrice == nil {
			variant.WSPrice = existing.WSPrice
		}
	}
	if variant.ID == "" { variant.ID = "mock-variant-uuid" }
	m.Variants[variant.ShopifyVariantID] = variant
	m.VariantsByID[variant.ID] = variant
	return nil
}

func (m *MockProductStore) UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64) error {
	if m.UpdateVariantPricesErr != nil { return m.UpdateVariantPricesErr }
	v, ok := m.VariantsByID[variantID]
	if !ok { return errors.New("variant not found") }
	v.WSPrice = wsPrice
	return nil
}

func (m *MockProductStore) GetProductByID(ctx context.Context, productID string) (*Product, error) {
	if m.GetProductByIDErr != nil { return nil, m.GetProductByIDErr }
	for _, p := range m.Products {
		if p.ID == productID {
			if !strings.EqualFold(p.Status, "active") {
				return nil, nil
			}
			return p, nil
		}
	}
	return nil, nil
}

func (m *MockProductStore) IsVariantFromActiveProduct(ctx context.Context, shopifyVariantID string) (bool, error) {
	v, ok := m.Variants[shopifyVariantID]
	if !ok {
		return false, nil
	}
	for _, p := range m.Products {
		if p.ID == v.ProductID && strings.EqualFold(p.Status, "active") {
			return true, nil
		}
	}
	return false, nil
}

func (m *MockProductStore) GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error) {
	if m.GetVariantsByProductIDErr != nil { return nil, m.GetVariantsByProductIDErr }
	out := []ProductVariant{}
	for _, v := range m.Variants {
		if v.ProductID == productID { out = append(out, *v) }
	}
	return out, nil
}

func (m *MockProductStore) UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) error {
	m.UpdateProductStatusCalls++
	return nil
}

func (m *MockProductStore) UpdateVariantFulfillmentType(ctx context.Context, variantID string, fulfillmentType string) error {
	v, ok := m.Variants[variantID]
	if !ok {
		return errors.New("variant not found")
	}
	v.FulfillmentType = fulfillmentType
	return nil
}

func (m *MockProductStore) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) error {
	m.UpdateVariantBatchLabelCalls++
	return nil
}

func (m *MockProductStore) UpdateSingleVariantBatchLabel(ctx context.Context, shopifyVariantID string, batchLabel string) error {
	v, ok := m.Variants[shopifyVariantID]
	if !ok {
		return errors.New("variant not found")
	}
	if batchLabel == "" {
		v.PreorderBatchLabel = nil
	} else {
		label := batchLabel
		v.PreorderBatchLabel = &label
	}
	return nil
}

func (m *MockProductStore) UpsertProductImages(ctx context.Context, productID string, images []ProductImage) error {
	return nil
}

func (m *MockProductStore) GetAllVariants(ctx context.Context) ([]ProductVariant, error) {
	out := []ProductVariant{}
	for _, v := range m.Variants {
		for _, p := range m.Products {
			if p.ID == v.ProductID && strings.EqualFold(p.Status, "active") {
				out = append(out, *v)
				break
			}
		}
	}
	return out, nil
}

func (m *MockProductStore) BulkUpdateVariantDimensions(ctx context.Context, inputs []DimensionUpdateInput) (BulkUpdateDimensionsResult, error) {
	var result BulkUpdateDimensionsResult
	for _, input := range inputs {
		v, ok := m.Variants[input.ShopifyVariantID]
		if !ok {
			result.NotFoundIDs = append(result.NotFoundIDs, input.ShopifyVariantID)
			continue
		}
		if !input.UpdateDimensions && input.WeightKg == nil {
			continue
		}
		if input.UpdateDimensions {
			v.WidthCm = input.WidthCm
			v.HeightCm = input.HeightCm
			v.DepthCm = input.DepthCm
		}
		if input.WeightKg != nil {
			v.WeightKg = *input.WeightKg
			result.WeightUpdatedCount++
		}
		result.UpdatedCount++
	}
	return result, nil
}

// MockShopifyClient is a test double for shopify.Client.
type MockShopifyClient struct {
	AdminGraphQLResponse []byte
	AdminGraphQLErr      error
	DraftOrderResponse   *shopify.DraftOrderResponse
	DraftOrderErr        error
	CartResponse     *shopify.CartCreateResponse
	CartErr          error
	GetVariantsInventoryResponse map[string]int
	GetVariantsInventoryErr      error
}

func (m *MockShopifyClient) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return m.AdminGraphQLResponse, m.AdminGraphQLErr
}

func (m *MockShopifyClient) CreateDraftOrder(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return m.DraftOrderResponse, m.DraftOrderErr
}

func (m *MockShopifyClient) SendDraftOrderInvoice(ctx context.Context, draftOrderID string, email *shopify.DraftOrderInvoiceEmailInput) error {
	return nil
}

func (m *MockShopifyClient) CreateStorefrontCart(ctx context.Context, input shopify.CartCreateInput) (*shopify.CartCreateResponse, error) {
	return &shopify.CartCreateResponse{CheckoutUrl: "mock-url"}, nil
}

func (m *MockShopifyClient) CreateRefund(ctx context.Context, shopifyOrderID string, amount float64, currency string, reason string) error {
	return nil
}

func (m *MockShopifyClient) GetVariantsInventory(ctx context.Context, variantIDs []string) (map[string]int, error) {
	return m.GetVariantsInventoryResponse, m.GetVariantsInventoryErr
}

func (m *MockShopifyClient) CreateFulfillment(ctx context.Context, shopifyOrderID string) error {
	return nil
}

func (m *MockShopifyClient) FetchFulfillmentOrders(ctx context.Context, shopifyOrderID string) ([]shopify.FulfillmentOrderData, error) {
	return nil, nil
}

func (m *MockShopifyClient) CreateFulfillmentV2(ctx context.Context, input shopify.CreateFulfillmentV2Input) (*shopify.CreateFulfillmentV2Result, error) {
	return &shopify.CreateFulfillmentV2Result{FulfillmentID: "gid://shopify/Fulfillment/mock"}, nil
}

func (m *MockShopifyClient) CreateFulfillmentEvent(ctx context.Context, shopifyFulfillmentID, status string) error {
	return nil
}

type MockShopifyClientFunc struct {
	QueryAdminGraphQLFn      func(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error)
	CreateDraftOrderFn       func(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error)
	CreateStorefrontCartFunc   func(ctx context.Context, input shopify.CartCreateInput) (*shopify.CartCreateResponse, error)
	CreateRefundFunc           func(ctx context.Context, shopifyOrderID string, amount float64, currency string, reason string) error
	GetVariantsInventoryFunc   func(ctx context.Context, variantIDs []string) (map[string]int, error)
}

func (m *MockShopifyClientFunc) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return m.QueryAdminGraphQLFn(ctx, query, variables)
}

func (m *MockShopifyClientFunc) CreateDraftOrder(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return m.CreateDraftOrderFn(ctx, input)
}

func (m *MockShopifyClientFunc) SendDraftOrderInvoice(ctx context.Context, draftOrderID string, email *shopify.DraftOrderInvoiceEmailInput) error {
	return nil
}

func (m *MockShopifyClientFunc) CreateStorefrontCart(ctx context.Context, input shopify.CartCreateInput) (*shopify.CartCreateResponse, error) {
	if m.CreateStorefrontCartFunc != nil {
		return m.CreateStorefrontCartFunc(ctx, input)
	}
	return &shopify.CartCreateResponse{CheckoutUrl: "mock-url"}, nil
}

func (m *MockShopifyClientFunc) CreateRefund(ctx context.Context, shopifyOrderID string, amount float64, currency string, reason string) error {
	if m.CreateRefundFunc != nil {
		return m.CreateRefundFunc(ctx, shopifyOrderID, amount, currency, reason)
	}
	return nil
}

func (m *MockShopifyClientFunc) GetVariantsInventory(ctx context.Context, variantIDs []string) (map[string]int, error) {
	if m.GetVariantsInventoryFunc != nil {
		return m.GetVariantsInventoryFunc(ctx, variantIDs)
	}
	return make(map[string]int), nil
}

func (m *MockShopifyClientFunc) CreateFulfillment(ctx context.Context, shopifyOrderID string) error {
	return nil
}

func (m *MockShopifyClientFunc) FetchFulfillmentOrders(ctx context.Context, shopifyOrderID string) ([]shopify.FulfillmentOrderData, error) {
	return nil, nil
}

func (m *MockShopifyClientFunc) CreateFulfillmentV2(ctx context.Context, input shopify.CreateFulfillmentV2Input) (*shopify.CreateFulfillmentV2Result, error) {
	return &shopify.CreateFulfillmentV2Result{FulfillmentID: "gid://shopify/Fulfillment/mock"}, nil
}

func (m *MockShopifyClientFunc) CreateFulfillmentEvent(ctx context.Context, shopifyFulfillmentID, status string) error {
	return nil
}
