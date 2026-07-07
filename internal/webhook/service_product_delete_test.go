package webhook

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
)

func TestHandleProductDeleteMarksProductAsDeleted(t *testing.T) {
	productStore := product.NewMockProductStore()
	productStore.Products["gid://shopify/Product/123"] = &product.Product{
		ID:        "product-123",
		ShopifyID: "gid://shopify/Product/123",
		Status:    "active",
	}

	svc := &service{productStore: productStore}
	if err := svc.HandleProductDelete(context.Background(), ShopifyProductDeleteWebhook{ID: 123}); err != nil {
		t.Fatalf("HandleProductDelete returned error: %v", err)
	}

	if productStore.Products["gid://shopify/Product/123"].Status != "deleted" {
		t.Fatalf("expected product status deleted, got %s", productStore.Products["gid://shopify/Product/123"].Status)
	}
}

