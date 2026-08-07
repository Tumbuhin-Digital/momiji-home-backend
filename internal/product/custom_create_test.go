package product_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preordercustomtext"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

func TestCreateCustomProduct_MultiVariantHappyPath(t *testing.T) {
	store := product.NewMockProductStore()
	client := &product.MockShopifyClient{}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	dto, err := svc.CreateCustomProduct(context.Background(), product.CreateCustomProductInput{
		Title:          "Custom Tee",
		IdempotencyKey: "key-1",
		Variants: []product.CreateCustomVariantInput{
			{Title: "Blue", RPPPrice: 20, WSPrice: 12},
			{Title: "Red", RPPPrice: 22, WSPrice: 14},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Origin != product.ProductOriginCustom {
		t.Fatalf("expected origin custom, got %s", dto.Origin)
	}
	if !strings.EqualFold(dto.Status, product.ProductStatusUnlisted) {
		t.Fatalf("expected unlisted status, got %s", dto.Status)
	}
	if len(dto.Variants) != 2 {
		t.Fatalf("expected 2 variants, got %d", len(dto.Variants))
	}
	if client.CreateUnlistedProductCalls != 1 {
		t.Fatalf("expected 1 shopify create call, got %d", client.CreateUnlistedProductCalls)
	}
	for _, v := range dto.Variants {
		if v.InventoryTracked {
			t.Fatal("expected inventory_tracked=false")
		}
		if v.CustomLinkState == nil || *v.CustomLinkState != product.CustomLinkStateAwaitingSKU {
			t.Fatalf("expected awaiting_sku, got %+v", v.CustomLinkState)
		}
		if v.FulfillmentType != product.FulfillmentTypePreOrder {
			t.Fatalf("expected pre_order, got %s", v.FulfillmentType)
		}
	}
}

func TestCreateCustomProduct_IdempotentRetry(t *testing.T) {
	store := product.NewMockProductStore()
	client := &product.MockShopifyClient{}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	input := product.CreateCustomProductInput{
		Title:          "Custom Tee",
		IdempotencyKey: "idem-retry",
		Variants:       []product.CreateCustomVariantInput{{Title: "Default Title", RPPPrice: 10, WSPrice: 8}},
	}
	first, err := svc.CreateCustomProduct(context.Background(), input)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	second, err := svc.CreateCustomProduct(context.Background(), input)
	if err != nil {
		t.Fatalf("second create failed: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected same product id, got %s vs %s", first.ID, second.ID)
	}
	if client.CreateUnlistedProductCalls != 1 {
		t.Fatalf("expected single shopify create, got %d", client.CreateUnlistedProductCalls)
	}
}

func TestCreateCustomProduct_ShopifyFailureRollsBack(t *testing.T) {
	store := product.NewMockProductStore()
	client := &product.MockShopifyClient{
		CreateUnlistedProductFn: func(ctx context.Context, input shopify.CreateUnlistedProductInput) (*shopify.CreatedProduct, error) {
			return nil, errors.New("shopify down")
		},
	}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	_, err := svc.CreateCustomProduct(context.Background(), product.CreateCustomProductInput{
		Title:          "Broken",
		IdempotencyKey: "fail-key",
		Variants:       []product.CreateCustomVariantInput{{Title: "Default Title", RPPPrice: 10, WSPrice: 8}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*apierror.AppError)
	if !ok || apiErr.Code != "shopify_error" {
		t.Fatalf("expected shopify_error, got %v", err)
	}
	products, total, _ := store.GetProducts(context.Background(), product.ProductQuery{})
	if total != 0 || len(products) != 0 {
		t.Fatalf("expected no listable products after failure, got %d", total)
	}
}

func TestGetProducts_IncludesUnlistedCustom(t *testing.T) {
	store := product.NewMockProductStore()
	awaiting := product.CustomLinkStateAwaitingSKU
	store.Products["gid://shopify/Product/u1"] = &product.Product{
		ID:        "p-u1",
		ShopifyID: "gid://shopify/Product/u1",
		Title:     "Unlisted Custom",
		Status:    product.ProductStatusUnlisted,
		Origin:    product.ProductOriginCustom,
		Variants: []product.ProductVariant{{
			ShopifyVariantID: "gid://shopify/ProductVariant/u1",
			Title:            "Default Title",
			Price:            10,
			FulfillmentType:  "ship_ready",
			InventoryTracked: false,
			CustomLinkState:  &awaiting,
		}},
	}
	store.Variants["gid://shopify/ProductVariant/u1"] = &store.Products["gid://shopify/Product/u1"].Variants[0]
	store.Variants["gid://shopify/ProductVariant/u1"].ProductID = "p-u1"

	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	products, total, err := svc.GetProducts(context.Background(), product.ProductQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(products) != 1 {
		t.Fatalf("expected unlisted product in listing, got total=%d len=%d", total, len(products))
	}
}

func TestGetProducts_FilterUnlisted(t *testing.T) {
	store := product.NewMockProductStore()
	store.Products["gid://shopify/Product/a"] = &product.Product{
		ID: "p-a", ShopifyID: "gid://shopify/Product/a", Title: "Active", Status: "active",
	}
	store.Products["gid://shopify/Product/u"] = &product.Product{
		ID: "p-u", ShopifyID: "gid://shopify/Product/u", Title: "Unlisted", Status: "unlisted", Origin: product.ProductOriginCustom,
	}
	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())

	products, total, err := svc.GetProducts(context.Background(), product.ProductQuery{FulfillmentType: "unlisted"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(products) != 1 {
		t.Fatalf("expected 1 unlisted product, got total=%d len=%d", total, len(products))
	}
	if products[0].Title != "Unlisted" {
		t.Fatalf("expected Unlisted product, got %s", products[0].Title)
	}
}


func TestSyncFromShopify_DoesNotFlipUntrackedToPreOrder(t *testing.T) {
	store := product.NewMockProductStore()
	awaiting := product.CustomLinkStateAwaitingSKU
	ws := 12.0
	store.Products["gid://shopify/Product/c1"] = &product.Product{
		ID:        "p-c1",
		ShopifyID: "gid://shopify/Product/c1",
		Title:     "Custom",
		Status:    product.ProductStatusUnlisted,
		Origin:    product.ProductOriginCustom,
	}
	store.Variants["gid://shopify/ProductVariant/c1"] = &product.ProductVariant{
		ID:                     "v-c1",
		ProductID:              "p-c1",
		ShopifyVariantID:       "gid://shopify/ProductVariant/c1",
		Title:                  "Default Title",
		Price:                  20,
		WSPrice:                &ws,
		FulfillmentType:        "ship_ready",
		InventoryQuantity:      0,
		InventoryTracked:       false,
		CustomLinkState:        &awaiting,
		ShopifyInventoryItemID: "gid://shopify/InventoryItem/c1",
	}

	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"products": map[string]interface{}{
				"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
				"edges": []map[string]interface{}{
					{
						"node": map[string]interface{}{
							"id":              "gid://shopify/Product/c1",
							"title":           "Custom",
							"descriptionHtml": "",
							"status":          "UNLISTED",
							"images":          map[string]interface{}{"edges": []interface{}{}},
							"variants": map[string]interface{}{
								"edges": []map[string]interface{}{
									{
										"node": map[string]interface{}{
											"id":                "gid://shopify/ProductVariant/c1",
											"title":             "Default Title",
											"sku":               "",
											"price":             "20.00",
											"inventoryQuantity": 0,
											"image":             map[string]interface{}{"url": ""},
											"inventoryItem": map[string]interface{}{
												"id":      "gid://shopify/InventoryItem/c1",
												"tracked": false,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	client := &product.MockShopifyClient{AdminGraphQLResponse: body}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	if err := svc.SyncFromShopify(context.Background()); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	v := store.Variants["gid://shopify/ProductVariant/c1"]
	if v.FulfillmentType != "ship_ready" {
		t.Fatalf("expected ship_ready preserved, got %s", v.FulfillmentType)
	}
	if v.WSPrice == nil || *v.WSPrice != 12 {
		t.Fatalf("expected ws_price preserved, got %+v", v.WSPrice)
	}
	if v.InventoryTracked {
		t.Fatal("expected inventory_tracked=false preserved")
	}
}

func TestSyncFromShopify_DoesNotSoftDeletePresentCustom(t *testing.T) {
	store := product.NewMockProductStore()
	store.Products["gid://shopify/Product/c2"] = &product.Product{
		ID:        "p-c2",
		ShopifyID: "gid://shopify/Product/c2",
		Title:     "Custom Keep",
		Status:    product.ProductStatusUnlisted,
		Origin:    product.ProductOriginCustom,
	}
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"products": map[string]interface{}{
				"pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
				"edges": []map[string]interface{}{
					{
						"node": map[string]interface{}{
							"id":              "gid://shopify/Product/c2",
							"title":           "Custom Keep",
							"descriptionHtml": "",
							"status":          "UNLISTED",
							"images":          map[string]interface{}{"edges": []interface{}{}},
							"variants": map[string]interface{}{
								"edges": []map[string]interface{}{
									{
										"node": map[string]interface{}{
											"id":                "gid://shopify/ProductVariant/c2",
											"title":             "Default Title",
											"sku":               "",
											"price":             "10.00",
											"inventoryQuantity": 0,
											"image":             map[string]interface{}{"url": ""},
											"inventoryItem":     map[string]interface{}{"id": "gid://shopify/InventoryItem/c2", "tracked": false},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	svc := product.NewProductService(store, &product.MockShopifyClient{AdminGraphQLResponse: body}, preordercustomtext.NewMockService())
	if err := svc.SyncFromShopify(context.Background()); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	p := store.Products["gid://shopify/Product/c2"]
	if strings.EqualFold(p.Status, "deleted") {
		t.Fatal("custom product was soft-deleted despite being present in Shopify")
	}
}

func TestLinkCustomVariantSKU_HappyPath(t *testing.T) {
	store := product.NewMockProductStore()
	awaiting := product.CustomLinkStateAwaitingSKU
	store.Products["gid://shopify/Product/link1"] = &product.Product{
		ID:        "p-link1",
		ShopifyID: "gid://shopify/Product/link1",
		Title:     "Custom Link",
		Status:    product.ProductStatusUnlisted,
		Origin:    product.ProductOriginCustom,
	}
	store.Variants["gid://shopify/ProductVariant/link1"] = &product.ProductVariant{
		ID:                     "v-link1",
		ProductID:              "p-link1",
		ShopifyVariantID:       "gid://shopify/ProductVariant/link1",
		Title:                  "Default Title",
		FulfillmentType:        "pre_order",
		InventoryTracked:       false,
		CustomLinkState:        &awaiting,
		ShopifyInventoryItemID: "gid://shopify/InventoryItem/link1",
	}

	client := &product.MockShopifyClient{}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	dto, err := svc.LinkCustomVariantSKU(context.Background(), "gid://shopify/ProductVariant/link1", "SM-SB-NT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.LinkVariantSKUCalls != 1 {
		t.Fatalf("expected 1 shopify link call, got %d", client.LinkVariantSKUCalls)
	}
	if client.LastLinkVariantSKUItemID != "gid://shopify/InventoryItem/link1" {
		t.Fatalf("unexpected inventory item id: %s", client.LastLinkVariantSKUItemID)
	}
	if client.LastLinkVariantSKU != "SM-SB-NT" {
		t.Fatalf("unexpected sku sent: %s", client.LastLinkVariantSKU)
	}
	if dto.SKU == nil || *dto.SKU != "SM-SB-NT" {
		t.Fatalf("expected sku on dto, got %+v", dto.SKU)
	}
	if !dto.InventoryTracked {
		t.Fatal("expected inventory_tracked=true")
	}
	if dto.CustomLinkState == nil || *dto.CustomLinkState != product.CustomLinkStateLinked {
		t.Fatalf("expected linked, got %+v", dto.CustomLinkState)
	}
	v := store.Variants["gid://shopify/ProductVariant/link1"]
	if v.SKU != "SM-SB-NT" || !v.InventoryTracked {
		t.Fatalf("store not updated: %+v", v)
	}
}

func TestLinkCustomVariantSKU_RejectsNonCustom(t *testing.T) {
	store := product.NewMockProductStore()
	awaiting := product.CustomLinkStateAwaitingSKU
	store.Products["gid://shopify/Product/sync1"] = &product.Product{
		ID: "p-sync1", ShopifyID: "gid://shopify/Product/sync1", Title: "Synced", Status: "active", Origin: product.ProductOriginShopifySync,
	}
	store.Variants["gid://shopify/ProductVariant/sync1"] = &product.ProductVariant{
		ProductID: "p-sync1", ShopifyVariantID: "gid://shopify/ProductVariant/sync1",
		CustomLinkState: &awaiting, ShopifyInventoryItemID: "gid://shopify/InventoryItem/sync1",
	}
	client := &product.MockShopifyClient{}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	_, err := svc.LinkCustomVariantSKU(context.Background(), "gid://shopify/ProductVariant/sync1", "ABC")
	if err == nil {
		t.Fatal("expected error")
	}
	if client.LinkVariantSKUCalls != 0 {
		t.Fatal("shopify should not be called")
	}
}

func TestLinkCustomVariantSKU_ShopifyFailureLeavesState(t *testing.T) {
	store := product.NewMockProductStore()
	awaiting := product.CustomLinkStateAwaitingSKU
	store.Products["gid://shopify/Product/fail1"] = &product.Product{
		ID: "p-fail1", ShopifyID: "gid://shopify/Product/fail1", Title: "Fail", Status: product.ProductStatusUnlisted, Origin: product.ProductOriginCustom,
	}
	store.Variants["gid://shopify/ProductVariant/fail1"] = &product.ProductVariant{
		ProductID: "p-fail1", ShopifyVariantID: "gid://shopify/ProductVariant/fail1",
		CustomLinkState: &awaiting, InventoryTracked: false, ShopifyInventoryItemID: "gid://shopify/InventoryItem/fail1",
	}
	client := &product.MockShopifyClient{
		LinkVariantSKUFn: func(ctx context.Context, inventoryItemID, sku string) error {
			return errors.New("shopify down")
		},
	}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	_, err := svc.LinkCustomVariantSKU(context.Background(), "gid://shopify/ProductVariant/fail1", "XYZ")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*apierror.AppError)
	if !ok || apiErr.Code != "shopify_error" {
		t.Fatalf("expected shopify_error, got %v", err)
	}
	v := store.Variants["gid://shopify/ProductVariant/fail1"]
	if v.CustomLinkState == nil || *v.CustomLinkState != product.CustomLinkStateAwaitingSKU {
		t.Fatalf("expected awaiting_sku preserved, got %+v", v.CustomLinkState)
	}
	if v.InventoryTracked || v.SKU != "" {
		t.Fatalf("expected local state unchanged, got tracked=%v sku=%q", v.InventoryTracked, v.SKU)
	}
}

func TestLinkCustomVariantSKU_AlreadyLinkedIdempotent(t *testing.T) {
	store := product.NewMockProductStore()
	linked := product.CustomLinkStateLinked
	store.Products["gid://shopify/Product/done1"] = &product.Product{
		ID: "p-done1", ShopifyID: "gid://shopify/Product/done1", Title: "Done", Status: product.ProductStatusUnlisted, Origin: product.ProductOriginCustom,
	}
	store.Variants["gid://shopify/ProductVariant/done1"] = &product.ProductVariant{
		ProductID: "p-done1", ShopifyVariantID: "gid://shopify/ProductVariant/done1",
		SKU: "EXISTING", CustomLinkState: &linked, InventoryTracked: true,
		ShopifyInventoryItemID: "gid://shopify/InventoryItem/done1",
	}
	client := &product.MockShopifyClient{}
	svc := product.NewProductService(store, client, preordercustomtext.NewMockService())

	dto, err := svc.LinkCustomVariantSKU(context.Background(), "gid://shopify/ProductVariant/done1", "NEW")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.LinkVariantSKUCalls != 0 {
		t.Fatal("expected no shopify call for already-linked")
	}
	if dto.SKU == nil || *dto.SKU != "EXISTING" {
		t.Fatalf("expected existing sku preserved, got %+v", dto.SKU)
	}
}
