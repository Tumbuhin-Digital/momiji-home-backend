package product

import (
	"context"
	"errors"

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
	for _, p := range m.Products { out = append(out, *p) }
	return out, int64(len(out)), nil
}

func (m *MockProductStore) GetVariantByShopifyID(ctx context.Context, id string) (*ProductVariant, error) {
	if m.GetVariantByShopifyIDErr != nil { return nil, m.GetVariantByShopifyIDErr }
	v, ok := m.Variants[id]
	if !ok { return nil, nil }
	return v, nil
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

func (m *MockProductStore) UpsertVariant(ctx context.Context, variant *ProductVariant) error {
	m.UpsertVariantCalls++
	if m.UpsertVariantErr != nil { return m.UpsertVariantErr }
	if variant.ID == "" { variant.ID = "mock-variant-uuid" }
	m.Variants[variant.ShopifyVariantID] = variant
	m.VariantsByID[variant.ID] = variant
	return nil
}

func (m *MockProductStore) UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64, retailPrice *float64) error {
	if m.UpdateVariantPricesErr != nil { return m.UpdateVariantPricesErr }
	v, ok := m.VariantsByID[variantID]
	if !ok { return errors.New("variant not found") }
	v.WSPrice = wsPrice
	v.RetailPrice = retailPrice
	return nil
}

func (m *MockProductStore) GetProductByID(ctx context.Context, productID string) (*Product, error) {
	if m.GetProductByIDErr != nil { return nil, m.GetProductByIDErr }
	for _, p := range m.Products {
		if p.ID == productID { return p, nil }
	}
	return nil, nil
}

func (m *MockProductStore) GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error) {
	if m.GetVariantsByProductIDErr != nil { return nil, m.GetVariantsByProductIDErr }
	out := []ProductVariant{}
	for _, v := range m.Variants {
		if v.ProductID == productID { out = append(out, *v) }
	}
	return out, nil
}

func (m *MockProductStore) UpdateProductStatus(ctx context.Context, productID string, status string) error {
	if m.UpdateProductStatusErr != nil { return m.UpdateProductStatusErr }
	for _, p := range m.Products {
		if p.ID == productID { p.Status = status; return nil }
	}
	return errors.New("product not found")
}

func (m *MockProductStore) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string) error {
	if m.UpdateVariantBatchLabelErr != nil { return m.UpdateVariantBatchLabelErr }
	for _, v := range m.Variants {
		if v.ProductID == productID { v.PreorderBatchLabel = &batchLabel }
	}
	return nil
}

// MockShopifyClient is a test double for shopify.Client.
type MockShopifyClient struct {
	AdminGraphQLResponse []byte
	AdminGraphQLErr      error
	DraftOrderResponse   *shopify.DraftOrderResponse
	DraftOrderErr        error
	CheckoutResponse     *shopify.CheckoutResponse
	CheckoutErr          error
}

func (m *MockShopifyClient) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return m.AdminGraphQLResponse, m.AdminGraphQLErr
}

func (m *MockShopifyClient) CreateDraftOrder(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return m.DraftOrderResponse, m.DraftOrderErr
}

func (m *MockShopifyClient) CreateStorefrontCheckout(ctx context.Context, input shopify.CheckoutCreateInput) (*shopify.CheckoutResponse, error) {
	return m.CheckoutResponse, m.CheckoutErr
}

type MockShopifyClientFunc struct {
	QueryAdminGraphQLFn        func(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error)
	CreateDraftOrderFn         func(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error)
	CreateStorefrontCheckoutFn func(ctx context.Context, input shopify.CheckoutCreateInput) (*shopify.CheckoutResponse, error)
}

func (m *MockShopifyClientFunc) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return m.QueryAdminGraphQLFn(ctx, query, variables)
}

func (m *MockShopifyClientFunc) CreateDraftOrder(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return m.CreateDraftOrderFn(ctx, input)
}

func (m *MockShopifyClientFunc) CreateStorefrontCheckout(ctx context.Context, input shopify.CheckoutCreateInput) (*shopify.CheckoutResponse, error) {
	return m.CreateStorefrontCheckoutFn(ctx, input)
}
