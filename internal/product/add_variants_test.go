package product_test

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preordercustomtext"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
)

func TestAddProductVariants_CustomProduct(t *testing.T) {
	store := product.NewMockProductStore()
	ws := 8.0
	store.Products["gid://shopify/Product/1"] = &product.Product{
		ID:        "prod-1",
		ShopifyID: "gid://shopify/Product/1",
		Title:     "Palawan Stool",
		Status:    product.ProductStatusUnlisted,
		Origin:    product.ProductOriginCustom,
		Variants: []product.ProductVariant{{
			ID:               "var-default",
			ProductID:        "prod-1",
			ShopifyVariantID: "gid://shopify/ProductVariant/default",
			Title:            "Default Title",
			Price:            10,
			WSPrice:          &ws,
			FulfillmentType:  "pre_order",
		}},
	}
	store.Variants["gid://shopify/ProductVariant/default"] = &store.Products["gid://shopify/Product/1"].Variants[0]

	client := &product.MockShopifyClient{}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	dto, err := svc.AddProductVariants(context.Background(), product.AddProductVariantsInput{
		ProductID:      "prod-1",
		IdempotencyKey: "idem-add-1",
		Variants: []product.CreateCustomVariantInput{
			{Title: "Natural", RPPPrice: 120, WSPrice: 90},
			{Title: "Walnut", RPPPrice: 130, WSPrice: 95},
		},
	})
	if err != nil {
		t.Fatalf("AddProductVariants: %v", err)
	}
	if client.AddProductVariantsCalls != 1 {
		t.Fatalf("expected 1 shopify add call, got %d", client.AddProductVariantsCalls)
	}
	if len(dto.Variants) < 2 {
		t.Fatalf("expected at least 2 variants in response, got %d", len(dto.Variants))
	}
	// Default Title should be removed locally after REMOVE_STANDALONE_VARIANT path.
	if _, ok := store.Variants["gid://shopify/ProductVariant/default"]; ok {
		t.Fatal("expected Default Title variant to be deleted locally")
	}
	added := store.Variants["gid://shopify/ProductVariant/added-1"]
	if added == nil {
		t.Fatal("expected first added variant in store")
	}
	if added.CustomLinkState == nil || *added.CustomLinkState != product.CustomLinkStateAwaitingSKU {
		t.Fatalf("expected awaiting_sku, got %v", added.CustomLinkState)
	}
	if added.InventoryTracked {
		t.Fatal("custom origin variants should be untracked")
	}
}

func TestAddProductVariants_ShopifySyncTracked(t *testing.T) {
	store := product.NewMockProductStore()
	store.Products["gid://shopify/Product/2"] = &product.Product{
		ID:        "prod-2",
		ShopifyID: "gid://shopify/Product/2",
		Title:     "Active Chair",
		Status:    product.ProductStatusActive,
		Origin:    product.ProductOriginShopifySync,
		Variants: []product.ProductVariant{{
			ID:               "var-a",
			ProductID:        "prod-2",
			ShopifyVariantID: "gid://shopify/ProductVariant/a",
			Title:            "Small",
			Price:            50,
			FulfillmentType:  "ship_ready",
			InventoryTracked: true,
		}},
	}
	store.Variants["gid://shopify/ProductVariant/a"] = &store.Products["gid://shopify/Product/2"].Variants[0]

	client := &product.MockShopifyClient{
		AddProductVariantsFn: func(ctx context.Context, input shopify.AddProductVariantsInput) ([]shopify.CreatedVariant, error) {
			if !input.InventoryTracked {
				t.Fatal("shopify_sync products should request tracked inventory")
			}
			return []shopify.CreatedVariant{{
				ID:                   "gid://shopify/ProductVariant/new",
				Title:                input.Variants[0].Title,
				Price:                input.Variants[0].Price,
				InventoryItemID:      "gid://shopify/InventoryItem/new",
				InventoryItemTracked: true,
			}}, nil
		},
		ListProductVariantIDsFn: func(ctx context.Context, productID string) ([]string, error) {
			return []string{
				"gid://shopify/ProductVariant/a",
				"gid://shopify/ProductVariant/new",
			}, nil
		},
	}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	_, err := svc.AddProductVariants(context.Background(), product.AddProductVariantsInput{
		ProductID:      "prod-2",
		IdempotencyKey: "idem-add-2",
		Variants:       []product.CreateCustomVariantInput{{Title: "Large", RPPPrice: 80, WSPrice: 60}},
	})
	if err != nil {
		t.Fatalf("AddProductVariants: %v", err)
	}
	added := store.Variants["gid://shopify/ProductVariant/new"]
	if added == nil {
		t.Fatal("expected new variant")
	}
	if added.CustomLinkState != nil {
		t.Fatalf("shopify_sync should not set custom_link_state, got %v", added.CustomLinkState)
	}
	if !added.InventoryTracked {
		t.Fatal("expected tracked inventory for shopify_sync")
	}
	// Existing named variant kept
	if _, ok := store.Variants["gid://shopify/ProductVariant/a"]; !ok {
		t.Fatal("existing variant should remain")
	}
}

func TestAddProductVariants_DuplicateTitle(t *testing.T) {
	store := product.NewMockProductStore()
	store.Products["gid://shopify/Product/3"] = &product.Product{
		ID:        "prod-3",
		ShopifyID: "gid://shopify/Product/3",
		Title:     "Dup Test",
		Status:    product.ProductStatusActive,
		Origin:    product.ProductOriginShopifySync,
		Variants: []product.ProductVariant{{
			ShopifyVariantID: "gid://shopify/ProductVariant/x",
			Title:            "Oak",
			ProductID:        "prod-3",
		}},
	}
	store.Variants["gid://shopify/ProductVariant/x"] = &store.Products["gid://shopify/Product/3"].Variants[0]
	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	_, err := svc.AddProductVariants(context.Background(), product.AddProductVariantsInput{
		ProductID:      "prod-3",
		IdempotencyKey: "idem-dup",
		Variants:       []product.CreateCustomVariantInput{{Title: "oak", RPPPrice: 1, WSPrice: 1}},
	})
	if err == nil {
		t.Fatal("expected duplicate title error")
	}
}
