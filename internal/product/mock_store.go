package product

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

// MockProductStore is an in-memory Store for unit tests.
type MockProductStore struct {
	Products     map[string]*Product        // keyed by ShopifyID
	Variants     map[string]*ProductVariant // keyed by ShopifyVariantID
	VariantsByID map[string]*ProductVariant // keyed by UUID ID

	GetProductByShopifyIDCalls   int
	UpsertProductCalls           int
	UpsertVariantCalls           int
	UpdateProductStatusCalls     int
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
	if m.GetVariantsErr != nil {
		return nil, 0, m.GetVariantsErr
	}
	out := make([]Product, 0, len(m.Products))
	wantedStatus := strings.ToLower(strings.TrimSpace(q.Status))
	if strings.EqualFold(q.FulfillmentType, "unlisted") {
		wantedStatus = "unlisted"
	}
	for _, p := range m.Products {
		status := strings.ToLower(p.Status)
		if status != "active" && status != "unlisted" {
			continue
		}
		if wantedStatus != "" && status != wantedStatus {
			continue
		}
		out = append(out, *p)
	}
	return out, int64(len(out)), nil
}

func (m *MockProductStore) ListShopifyProductIDs(ctx context.Context) ([]string, error) {
	ids := make([]string, 0, len(m.Products))
	for _, p := range m.Products {
		status := strings.ToLower(p.Status)
		if status == "deleted" || status == "creating" || status == "failed" {
			continue
		}
		if strings.HasPrefix(p.ShopifyID, "pending:") {
			continue
		}
		ids = append(ids, p.ShopifyID)
	}
	return ids, nil
}

func (m *MockProductStore) GetVariantByShopifyID(ctx context.Context, id string) (*ProductVariant, error) {
	if m.GetVariantByShopifyIDErr != nil {
		return nil, m.GetVariantByShopifyIDErr
	}
	v, ok := m.Variants[id]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (m *MockProductStore) GetVariantByInventoryItemID(ctx context.Context, id string) (*ProductVariant, error) {
	if m.GetVariantByShopifyIDErr != nil {
		return nil, m.GetVariantByShopifyIDErr
	} // reuse err for simplicity
	for _, v := range m.Variants {
		if v.ShopifyInventoryItemID == id {
			return v, nil
		}
	}
	return nil, nil
}

func (m *MockProductStore) GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error) {
	m.GetProductByShopifyIDCalls++
	if m.GetProductByShopifyIDErr != nil {
		return nil, m.GetProductByShopifyIDErr
	}
	p, ok := m.Products[shopifyID]
	if !ok {
		return nil, nil
	}
	return p, nil
}

func (m *MockProductStore) UpsertProduct(ctx context.Context, product *Product) error {
	m.UpsertProductCalls++
	if m.UpsertProductErr != nil {
		return m.UpsertProductErr
	}
	if product.ID == "" {
		product.ID = "mock-product-uuid"
	}
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
	if m.UpsertVariantErr != nil {
		return m.UpsertVariantErr
	}
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
	if variant.ID == "" {
		variant.ID = "mock-variant-uuid"
	}
	m.Variants[variant.ShopifyVariantID] = variant
	m.VariantsByID[variant.ID] = variant
	return nil
}

func (m *MockProductStore) UpdateInventoryQuantity(ctx context.Context, shopifyVariantID string, quantity int) error {
	if v, ok := m.Variants[shopifyVariantID]; ok {
		v.InventoryQuantity = quantity
	}
	return nil
}

func (m *MockProductStore) UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64) error {
	if m.UpdateVariantPricesErr != nil {
		return m.UpdateVariantPricesErr
	}
	v, ok := m.VariantsByID[variantID]
	if !ok {
		return errors.New("variant not found")
	}
	v.WSPrice = wsPrice
	return nil
}

func (m *MockProductStore) UpdateVariantLtl(ctx context.Context, variantID string, isLtl bool) error {
	v, ok := m.Variants[variantID]
	if !ok {
		return errors.New("variant not found")
	}
	v.IsLtl = isLtl
	return nil
}

func (m *MockProductStore) GetProductByID(ctx context.Context, productID string) (*Product, error) {
	if m.GetProductByIDErr != nil {
		return nil, m.GetProductByIDErr
	}
	for _, p := range m.Products {
		if p.ID == productID {
			status := strings.ToLower(p.Status)
			if status != "active" && status != "unlisted" {
				return nil, nil
			}
			cp := *p
			cp.Variants = nil
			for _, v := range m.Variants {
				if v.ProductID == p.ID {
					cp.Variants = append(cp.Variants, *v)
				}
			}
			return &cp, nil
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
		if p.ID == v.ProductID {
			status := strings.ToLower(p.Status)
			if status == "active" || status == "unlisted" {
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *MockProductStore) GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error) {
	if m.GetVariantsByProductIDErr != nil {
		return nil, m.GetVariantsByProductIDErr
	}
	out := []ProductVariant{}
	for _, v := range m.Variants {
		if v.ProductID == productID {
			out = append(out, *v)
		}
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
			if p.ID == v.ProductID {
				status := strings.ToLower(p.Status)
				if status == "active" || status == "unlisted" {
					out = append(out, *v)
					break
				}
			}
		}
	}
	return out, nil
}

func (m *MockProductStore) GetBatchSummariesByVariantIDs(ctx context.Context, variantIDs []string) (map[string]VariantBatchSummary, error) {
	result := make(map[string]VariantBatchSummary, len(variantIDs))
	for _, variantID := range variantIDs {
		if v, ok := m.Variants[variantID]; ok && v.BatchSummary != nil {
			result[variantID] = *v.BatchSummary
		}
	}
	return result, nil
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

func (m *MockProductStore) GetProductByIdempotencyKey(ctx context.Context, key string) (*Product, error) {
	for _, p := range m.Products {
		if p.CreateIdempotencyKey != nil && *p.CreateIdempotencyKey == key {
			cp := *p
			cp.Variants = nil
			for _, v := range m.Variants {
				if v.ProductID == p.ID {
					cp.Variants = append(cp.Variants, *v)
				}
			}
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *MockProductStore) CreateCustomProductStub(ctx context.Context, product *Product) error {
	if product.ID == "" {
		product.ID = "mock-custom-" + product.ShopifyID
	}
	if product.CreateIdempotencyKey != nil {
		for _, p := range m.Products {
			if p.CreateIdempotencyKey != nil && *p.CreateIdempotencyKey == *product.CreateIdempotencyKey {
				return errors.New("duplicate idempotency key")
			}
		}
	}
	cp := *product
	m.Products[product.ShopifyID] = &cp
	return nil
}

func (m *MockProductStore) FinalizeCustomProduct(ctx context.Context, productID string, product *Product, variants []ProductVariant, images []ProductImage) error {
	var existing *Product
	var oldKey string
	for k, p := range m.Products {
		if p.ID == productID {
			existing = p
			oldKey = k
			break
		}
	}
	if existing == nil {
		return errors.New("product stub not found")
	}
	existing.ShopifyID = product.ShopifyID
	existing.Title = product.Title
	existing.Status = product.Status
	existing.Handle = product.Handle
	existing.Origin = product.Origin
	existing.InternalNote = product.InternalNote
	if oldKey != product.ShopifyID {
		delete(m.Products, oldKey)
		m.Products[product.ShopifyID] = existing
	}
	for k, v := range m.Variants {
		if v.ProductID == productID {
			delete(m.Variants, k)
		}
	}
	for i := range variants {
		v := variants[i]
		v.ProductID = productID
		if v.ID == "" {
			v.ID = "mock-variant-" + v.ShopifyVariantID
		}
		cp := v
		m.Variants[v.ShopifyVariantID] = &cp
	}
	existing.Images = images
	return nil
}

func (m *MockProductStore) MarkCustomProductFailed(ctx context.Context, productID string) error {
	for _, p := range m.Products {
		if p.ID == productID {
			p.Status = ProductStatusFailed
			return nil
		}
	}
	return errors.New("product not found")
}

func (m *MockProductStore) DeleteProductByID(ctx context.Context, productID string) error {
	for k, p := range m.Products {
		if p.ID == productID {
			delete(m.Products, k)
			for vk, v := range m.Variants {
				if v.ProductID == productID {
					delete(m.Variants, vk)
				}
			}
			return nil
		}
	}
	return nil
}

func (m *MockProductStore) MarkCustomVariantLinked(ctx context.Context, shopifyVariantID, sku string) error {
	v, ok := m.Variants[shopifyVariantID]
	if !ok {
		return errors.New("variant not found")
	}
	linked := CustomLinkStateLinked
	v.SKU = sku
	v.CustomLinkState = &linked
	v.InventoryTracked = true
	return nil
}

// MockShopifyClient is a test double for shopify.Client.
type MockShopifyClient struct {
	AdminGraphQLResponse         []byte
	AdminGraphQLErr              error
	DraftOrderResponse           *shopify.DraftOrderResponse
	DraftOrderErr                error
	CartResponse                 *shopify.CartCreateResponse
	CartErr                      error
	GetVariantsInventoryResponse map[string]int
	GetVariantsInventoryErr      error
	CreateUnlistedProductFn      func(ctx context.Context, input shopify.CreateUnlistedProductInput) (*shopify.CreatedProduct, error)
	CreateUnlistedProductCalls   int
	LinkVariantSKUFn             func(ctx context.Context, inventoryItemID, sku string) error
	LinkVariantSKUCalls          int
	LastLinkVariantSKUItemID     string
	LastLinkVariantSKU           string
	AttachMediaURLFn             func(ctx context.Context, productID, imageURL, alt string) (*shopify.CreatedProductMedia, error)
	AttachMediaBytesFn           func(ctx context.Context, productID, filename, contentType string, data []byte, alt string) (*shopify.CreatedProductMedia, error)
}

func (m *MockShopifyClient) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return m.AdminGraphQLResponse, m.AdminGraphQLErr
}

func (m *MockShopifyClient) CreateDraftOrder(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return m.DraftOrderResponse, m.DraftOrderErr
}

func (m *MockShopifyClient) DeleteDraftOrder(ctx context.Context, draftOrderID string) error {
	return nil
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

func (m *MockShopifyClient) CreateUnlistedProduct(ctx context.Context, input shopify.CreateUnlistedProductInput) (*shopify.CreatedProduct, error) {
	m.CreateUnlistedProductCalls++
	if m.CreateUnlistedProductFn != nil {
		return m.CreateUnlistedProductFn(ctx, input)
	}
	variants := make([]shopify.CreatedVariant, 0, len(input.Variants))
	for i, v := range input.Variants {
		variants = append(variants, shopify.CreatedVariant{
			ID:                fmt.Sprintf("gid://shopify/ProductVariant/%d", i+1),
			Title:             v.Title,
			Price:             v.Price,
			InventoryItemID:   fmt.Sprintf("gid://shopify/InventoryItem/%d", i+1),
			InventoryQuantity: 0,
		})
	}
	return &shopify.CreatedProduct{
		ID:       "gid://shopify/Product/custom-1",
		Title:    input.Title,
		Status:   "unlisted",
		Handle:   "custom-product",
		Variants: variants,
	}, nil
}

func (m *MockShopifyClient) AttachProductMediaFromURL(ctx context.Context, productID, imageURL, alt string) (*shopify.CreatedProductMedia, error) {
	if m.AttachMediaURLFn != nil {
		return m.AttachMediaURLFn(ctx, productID, imageURL, alt)
	}
	return &shopify.CreatedProductMedia{ID: "gid://shopify/MediaImage/1", URL: imageURL, Alt: alt}, nil
}

func (m *MockShopifyClient) AttachProductMediaFromBytes(ctx context.Context, productID, filename, contentType string, data []byte, alt string) (*shopify.CreatedProductMedia, error) {
	if m.AttachMediaBytesFn != nil {
		return m.AttachMediaBytesFn(ctx, productID, filename, contentType, data, alt)
	}
	return &shopify.CreatedProductMedia{ID: "gid://shopify/MediaImage/1", URL: "https://cdn.example.com/" + filename, Alt: alt}, nil
}

func (m *MockShopifyClient) LinkVariantSKU(ctx context.Context, inventoryItemID, sku string) error {
	m.LinkVariantSKUCalls++
	m.LastLinkVariantSKUItemID = inventoryItemID
	m.LastLinkVariantSKU = sku
	if m.LinkVariantSKUFn != nil {
		return m.LinkVariantSKUFn(ctx, inventoryItemID, sku)
	}
	return nil
}

type MockShopifyClientFunc struct {
	QueryAdminGraphQLFn      func(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error)
	CreateDraftOrderFn       func(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error)
	CreateStorefrontCartFunc func(ctx context.Context, input shopify.CartCreateInput) (*shopify.CartCreateResponse, error)
	CreateRefundFunc         func(ctx context.Context, shopifyOrderID string, amount float64, currency string, reason string) error
	GetVariantsInventoryFunc func(ctx context.Context, variantIDs []string) (map[string]int, error)
	CreateUnlistedProductFn  func(ctx context.Context, input shopify.CreateUnlistedProductInput) (*shopify.CreatedProduct, error)
}

func (m *MockShopifyClientFunc) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
	return m.QueryAdminGraphQLFn(ctx, query, variables)
}

func (m *MockShopifyClientFunc) CreateDraftOrder(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
	return m.CreateDraftOrderFn(ctx, input)
}

func (m *MockShopifyClientFunc) DeleteDraftOrder(ctx context.Context, draftOrderID string) error {
	return nil
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

func (m *MockShopifyClientFunc) CreateUnlistedProduct(ctx context.Context, input shopify.CreateUnlistedProductInput) (*shopify.CreatedProduct, error) {
	if m.CreateUnlistedProductFn != nil {
		return m.CreateUnlistedProductFn(ctx, input)
	}
	return &shopify.CreatedProduct{
		ID:     "gid://shopify/Product/func-1",
		Title:  input.Title,
		Status: "unlisted",
		Variants: []shopify.CreatedVariant{{
			ID:              "gid://shopify/ProductVariant/func-1",
			Title:           input.Variants[0].Title,
			Price:           input.Variants[0].Price,
			InventoryItemID: "gid://shopify/InventoryItem/func-1",
		}},
	}, nil
}

func (m *MockShopifyClientFunc) AttachProductMediaFromURL(ctx context.Context, productID, imageURL, alt string) (*shopify.CreatedProductMedia, error) {
	return &shopify.CreatedProductMedia{ID: "gid://shopify/MediaImage/1", URL: imageURL, Alt: alt}, nil
}

func (m *MockShopifyClientFunc) AttachProductMediaFromBytes(ctx context.Context, productID, filename, contentType string, data []byte, alt string) (*shopify.CreatedProductMedia, error) {
	return &shopify.CreatedProductMedia{ID: "gid://shopify/MediaImage/1", URL: "https://cdn.example.com/" + filename, Alt: alt}, nil
}

func (m *MockShopifyClientFunc) LinkVariantSKU(ctx context.Context, inventoryItemID, sku string) error {
	return nil
}
