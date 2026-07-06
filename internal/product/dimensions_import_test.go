package product_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
)

const shopifyWeightKg = 6.35 // 14 lb from Shopify

func TestBulkUpdateDimensionsPreservesShopifyWeight(t *testing.T) {
	store := product.NewMockProductStore()
	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WeightKg:         shopifyWeightKg,
		WidthCm:          1,
		HeightCm:         1,
		DepthCm:          1,
	}

	svc := product.NewProductService(store, &product.MockShopifyClient{})
	err := svc.BulkUpdateDimensions(context.Background(), []product.DimensionUpdateInput{{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WidthCm:          units.InToCm(10),
		HeightCm:         units.InToCm(15),
		DepthCm:          units.InToCm(15),
	}})
	if err != nil {
		t.Fatalf("BulkUpdateDimensions failed: %v", err)
	}

	v := store.Variants["gid://shopify/ProductVariant/1"]
	if v.WeightKg != shopifyWeightKg {
		t.Fatalf("expected weight_kg %.2f unchanged, got %.2f", shopifyWeightKg, v.WeightKg)
	}
	if v.WidthCm != units.InToCm(10) || v.HeightCm != units.InToCm(15) || v.DepthCm != units.InToCm(15) {
		t.Fatalf("expected dimensions 10x15x15 in, got %.2f x %.2f x %.2f cm", v.WidthCm, v.HeightCm, v.DepthCm)
	}
}

func TestImportDimensionsIgnoresWeightColumn(t *testing.T) {
	store := product.NewMockProductStore()
	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WeightKg:         shopifyWeightKg,
	}

	svc := product.NewProductService(store, &product.MockShopifyClient{})
	h := product.NewProductHandler(svc, "test-secret")

	app := fiber.New()
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	csv := "variant_id,product_title,variant_title,sku,weight_lb,width_in,height_in,depth_in\n" +
		"gid://shopify/ProductVariant/1,Pumpkin Basket,Default,SKU-1,99.00,10.00,15.00,15.00\n"

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "dimensions.csv")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := io.WriteString(part, csv); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/api/v1/products/variants/dimensions/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminTestToken(t))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	v := store.Variants["gid://shopify/ProductVariant/1"]
	if v.WeightKg != shopifyWeightKg {
		t.Fatalf("expected Shopify weight %.2f preserved, got %.2f", shopifyWeightKg, v.WeightKg)
	}
	if v.WidthCm != units.InToCm(10) || v.HeightCm != units.InToCm(15) || v.DepthCm != units.InToCm(15) {
		t.Fatalf("expected dimensions updated to 10x15x15 in, got %.2f x %.2f x %.2f cm", v.WidthCm, v.HeightCm, v.DepthCm)
	}
}

func adminTestToken(t *testing.T) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "test-admin",
		"role": "admin",
	})
	signed, err := token.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}
