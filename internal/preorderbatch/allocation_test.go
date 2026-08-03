package preorderbatch

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
)

type stubAllocStore struct {
	Store
}

func TestPreviewAllocation_ShipReadyOverflowSkipsBatchRequirement(t *testing.T) {
	t.Parallel()

	productStore := product.NewMockProductStore()
	productStore.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ID:               "variant-uuid",
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		FulfillmentType:  string(product.FulfillmentTypeShipReady),
		InventoryQuantity: 6,
	}

	svc := NewService(&stubAllocStore{}, productStore, nil)
	result, err := svc.PreviewAllocation(context.Background(), "gid://shopify/ProductVariant/1", 1, nil, nil)
	if err != nil {
		t.Fatalf("PreviewAllocation() error = %v, want nil for ship_ready overflow", err)
	}
	if result == nil {
		t.Fatal("PreviewAllocation() result is nil")
	}
	if result.UnlimitedQty != 1 {
		t.Fatalf("UnlimitedQty = %d, want 1", result.UnlimitedQty)
	}
	if result.Depletion != nil {
		t.Fatalf("Depletion = %#v, want nil", result.Depletion)
	}
}

func TestRequirePreorderVariant_StillRejectsShipReady(t *testing.T) {
	t.Parallel()

	productStore := product.NewMockProductStore()
	productStore.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ID:               "variant-uuid",
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		FulfillmentType:  string(product.FulfillmentTypeShipReady),
	}

	svc := &service{productStore: productStore}
	_, err := svc.requirePreorderVariant(context.Background(), "gid://shopify/ProductVariant/1")
	if err == nil {
		t.Fatal("requirePreorderVariant() error = nil, want invalid_variant_status")
	}
}
