package product_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

func buildShopifyProductResponse(shopifyProductID, shopifyVariantID, title, price string) []byte {
	return buildShopifyProductResponseWithPageInfo(shopifyProductID, shopifyVariantID, false, "")
}

func buildShopifyProductResponseWithPageInfo(shopifyProductID, shopifyVariantID string, hasNextPage bool, endCursor string) []byte {
	resp := map[string]interface{}{
		"data": map[string]interface{}{
			"products": map[string]interface{}{
				"edges": []map[string]interface{}{
					{
						"node": map[string]interface{}{
							"id": shopifyProductID, "title": "Test Title",
							"descriptionHtml": "", "status": "ACTIVE",
							"variants": map[string]interface{}{
								"edges": []map[string]interface{}{
									{"node": map[string]interface{}{
										"id": shopifyVariantID, "title": "Test Variant Title",
										"sku": "SKU-001", "price": "10.00",
										"inventoryQuantity": 50,
										"image":             map[string]string{"url": ""},
									}},
								},
							},
						},
					},
				},
				"pageInfo": map[string]interface{}{"hasNextPage": hasNextPage, "endCursor": endCursor},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

func TestGetVariantByID_Found(t *testing.T) {
	store := product.NewMockProductStore()
	svc := product.NewProductService(store, &product.MockShopifyClient{})

	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ID: "uuid-1", ShopifyVariantID: "gid://shopify/ProductVariant/1",
		Title: "Test Variant", Price: 100.00, FulfillmentType: "ship_ready",
	}

	dto, err := svc.GetVariantByID(context.Background(), "gid://shopify/ProductVariant/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Title != "Test Variant" {
		t.Fatalf("expected 'Test Variant', got '%s'", dto.Title)
	}
}

func TestGetVariantByID_NotFound(t *testing.T) {
	store := product.NewMockProductStore()
	svc := product.NewProductService(store, &product.MockShopifyClient{})

	_, err := svc.GetVariantByID(context.Background(), "nonexistent")
	if err != apierror.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSyncFromShopify_Success(t *testing.T) {
	store := product.NewMockProductStore()
	mockClient := &product.MockShopifyClient{
		AdminGraphQLResponse: buildShopifyProductResponse(
			"gid://shopify/Product/1", "gid://shopify/ProductVariant/1", "Rose Mug", "45.00",
		),
	}
	svc := product.NewProductService(store, mockClient)

	err := svc.SyncFromShopify(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.UpsertProductCalls != 1 {
		t.Fatalf("expected 1 UpsertProduct call, got %d", store.UpsertProductCalls)
	}
	if store.UpsertVariantCalls != 1 {
		t.Fatalf("expected 1 UpsertVariant call, got %d", store.UpsertVariantCalls)
	}
}

func TestSyncFromShopify_ClientError(t *testing.T) {
	store := product.NewMockProductStore()
	mockClient := &product.MockShopifyClient{AdminGraphQLErr: errors.New("shopify timeout")}
	svc := product.NewProductService(store, mockClient)

	err := svc.SyncFromShopify(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSyncFromShopify_MultiPage(t *testing.T) {
	store := product.NewMockProductStore()
	callCount := 0
	responses := [][]byte{
		buildShopifyProductResponseWithPageInfo(
			"gid://shopify/Product/1", "gid://shopify/ProductVariant/1",
			true, "cursor-abc",
		),
		buildShopifyProductResponseWithPageInfo(
			"gid://shopify/Product/2", "gid://shopify/ProductVariant/2",
			false, "",
		),
	}
	mockClient := &product.MockShopifyClientFunc{
		QueryAdminGraphQLFn: func(ctx context.Context, query string, vars map[string]interface{}) ([]byte, error) {
			resp := responses[callCount]
			callCount++
			return resp, nil
		},
	}
	svc := product.NewProductService(store, mockClient)

	err := svc.SyncFromShopify(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.UpsertProductCalls != 2 {
		t.Fatalf("expected 2 UpsertProduct calls, got %d", store.UpsertProductCalls)
	}
}

func TestSyncFromShopify_MarksMissingProductsAsDeleted(t *testing.T) {
	store := product.NewMockProductStore()
	store.Products["gid://shopify/Product/1"] = &product.Product{
		ID:        "product-1",
		ShopifyID: "gid://shopify/Product/1",
		Title:     "Product 1",
		Status:    "active",
	}
	store.Products["gid://shopify/Product/2"] = &product.Product{
		ID:        "product-2",
		ShopifyID: "gid://shopify/Product/2",
		Title:     "Product 2",
		Status:    "active",
	}

	mockClient := &product.MockShopifyClient{
		AdminGraphQLResponse: buildShopifyProductResponse(
			"gid://shopify/Product/1", "gid://shopify/ProductVariant/1", "Rose Mug", "45.00",
		),
	}
	svc := product.NewProductService(store, mockClient)

	if err := svc.SyncFromShopify(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if store.Products["gid://shopify/Product/1"].Status == "deleted" {
		t.Fatal("expected Shopify product still present to remain not deleted")
	}
	if store.Products["gid://shopify/Product/2"].Status != "deleted" {
		t.Fatalf("expected missing product to be marked deleted, got status=%s", store.Products["gid://shopify/Product/2"].Status)
	}
}

func TestGetProductByID_NotFound(t *testing.T) {
	store := product.NewMockProductStore()
	svc := product.NewProductService(store, &product.MockShopifyClient{})

	_, err := svc.GetProductByID(context.Background(), "nonexistent-uuid")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUpdateProductStatus_InvalidStatus(t *testing.T) {
	store := product.NewMockProductStore()
	svc := product.NewProductService(store, &product.MockShopifyClient{})

	_, err := svc.UpdateProductStatus(context.Background(), "any-id", "invalid_status")
	if err == nil {
		t.Fatal("expected validation error for invalid status")
	}
}
