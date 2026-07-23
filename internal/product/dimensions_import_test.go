package product_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preordercustomtext"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
	"github.com/xuri/excelize/v2"
)

const existingWeightKg = 6.35 // ~14 lb

func TestBulkUpdateDimensionsPreservesWeightWhenNotProvided(t *testing.T) {
	store := product.NewMockProductStore()
	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WeightKg:         existingWeightKg,
		WidthCm:          1,
		HeightCm:         1,
		DepthCm:          1,
	}

	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	_, err := svc.BulkUpdateDimensions(context.Background(), []product.DimensionUpdateInput{{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WidthCm:          units.InToCm(10),
		HeightCm:         units.InToCm(15),
		DepthCm:          units.InToCm(15),
		UpdateDimensions: true,
	}})
	if err != nil {
		t.Fatalf("BulkUpdateDimensions failed: %v", err)
	}

	v := store.Variants["gid://shopify/ProductVariant/1"]
	if v.WeightKg != existingWeightKg {
		t.Fatalf("expected weight_kg %.2f unchanged, got %.2f", existingWeightKg, v.WeightKg)
	}
	if v.WidthCm != units.InToCm(10) || v.HeightCm != units.InToCm(15) || v.DepthCm != units.InToCm(15) {
		t.Fatalf("expected dimensions 10x15x15 in, got %.2f x %.2f x %.2f cm", v.WidthCm, v.HeightCm, v.DepthCm)
	}
}

func TestImportPackagingUpdatesWeightLb(t *testing.T) {
	store := product.NewMockProductStore()
	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WeightKg:         existingWeightKg,
	}

	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	h := product.NewProductHandler(svc, "test-secret")

	app := fiber.New()
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	csv := "variant_id,product_title,variant_title,sku,weight_lb,length_in,width_in,height_in\n" +
		"gid://shopify/ProductVariant/1,Pumpkin Basket,Default,SKU-1,99.00,15.00,10.00,15.00\n"

	resp := postPackagingCSV(t, app, csv)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeSuccessData(t, resp)
	if int(body["updated_count"].(float64)) != 1 {
		t.Fatalf("expected updated_count 1, got %v", body["updated_count"])
	}
	if int(body["weight_updated_count"].(float64)) != 1 {
		t.Fatalf("expected weight_updated_count 1, got %v", body["weight_updated_count"])
	}

	v := store.Variants["gid://shopify/ProductVariant/1"]
	expectedKg := units.LbToKg(99)
	if math.Abs(v.WeightKg-expectedKg) > 0.0001 {
		t.Fatalf("expected weight_kg %.4f from 99 lb, got %.4f", expectedKg, v.WeightKg)
	}
	if v.WidthCm != units.InToCm(10) || v.HeightCm != units.InToCm(15) || v.DepthCm != units.InToCm(15) {
		t.Fatalf("expected dimensions updated to 10x15x15 in, got %.2f x %.2f x %.2f cm", v.WidthCm, v.HeightCm, v.DepthCm)
	}
}

func TestImportPackagingEmptyWeightSkipsWeightUpdate(t *testing.T) {
	store := product.NewMockProductStore()
	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WeightKg:         existingWeightKg,
	}

	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	h := product.NewProductHandler(svc, "test-secret")

	app := fiber.New()
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	csv := "variant_id,product_title,variant_title,sku,weight_lb,length_in,width_in,height_in\n" +
		"gid://shopify/ProductVariant/1,Pumpkin Basket,Default,SKU-1,,15.00,10.00,15.00\n"

	resp := postPackagingCSV(t, app, csv)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	v := store.Variants["gid://shopify/ProductVariant/1"]
	if v.WeightKg != existingWeightKg {
		t.Fatalf("expected weight preserved at %.2f, got %.2f", existingWeightKg, v.WeightKg)
	}
}

func TestImportPackagingRejectsInvalidWeight(t *testing.T) {
	store := product.NewMockProductStore()
	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WeightKg:         existingWeightKg,
		WidthCm:          5,
		HeightCm:         5,
		DepthCm:          5,
	}

	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	h := product.NewProductHandler(svc, "test-secret")

	app := fiber.New()
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	csv := "variant_id,product_title,variant_title,sku,weight_lb,length_in,width_in,height_in\n" +
		"gid://shopify/ProductVariant/1,Pumpkin Basket,Default,SKU-1,-1,15.00,10.00,15.00\n" +
		"gid://shopify/ProductVariant/1,Pumpkin Basket,Default,SKU-1,abc,20.00,12.00,16.00\n"

	resp := postPackagingCSV(t, app, csv)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeSuccessData(t, resp)
	errs, ok := body["errors"].([]interface{})
	if !ok || len(errs) < 2 {
		t.Fatalf("expected at least 2 errors, got %v", body["errors"])
	}

	v := store.Variants["gid://shopify/ProductVariant/1"]
	if v.WeightKg != existingWeightKg {
		t.Fatalf("expected weight unchanged after invalid rows, got %.2f", v.WeightKg)
	}
	// Invalid weight rows still apply dimensions when dim columns are present.
	if v.WidthCm != units.InToCm(12) || v.HeightCm != units.InToCm(16) || v.DepthCm != units.InToCm(20) {
		t.Fatalf("expected last valid dim row applied, got %.2f x %.2f x %.2f cm", v.WidthCm, v.HeightCm, v.DepthCm)
	}
}

func TestImportPackagingUnknownVariantReportsError(t *testing.T) {
	store := product.NewMockProductStore()
	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	h := product.NewProductHandler(svc, "test-secret")

	app := fiber.New()
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	csv := "variant_id,product_title,variant_title,sku,weight_lb,length_in,width_in,height_in\n" +
		"gid://shopify/ProductVariant/missing,X,Y,SKU,14.00,15.00,10.00,15.00\n"

	resp := postPackagingCSV(t, app, csv)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := decodeSuccessData(t, resp)
	if int(body["updated_count"].(float64)) != 0 {
		t.Fatalf("expected updated_count 0, got %v", body["updated_count"])
	}
	errs, ok := body["errors"].([]interface{})
	if !ok || len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", body["errors"])
	}
	err0 := errs[0].(map[string]interface{})
	if err0["message"] != "variant_id not found" {
		t.Fatalf("unexpected error message: %v", err0["message"])
	}
}

func TestImportDimensionsSupportsLegacyDepthHeader(t *testing.T) {
	store := product.NewMockProductStore()
	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		WeightKg:         existingWeightKg,
	}

	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	h := product.NewProductHandler(svc, "test-secret")

	app := fiber.New()
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	csv := "variant_id,product_title,variant_title,sku,width_in,height_in,depth_in\n" +
		"gid://shopify/ProductVariant/1,Pumpkin Basket,Default,SKU-1,10.00,15.00,15.00\n"

	resp := postPackagingCSV(t, app, csv)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	v := store.Variants["gid://shopify/ProductVariant/1"]
	if v.WeightKg != existingWeightKg {
		t.Fatalf("expected weight %.2f preserved, got %.2f", existingWeightKg, v.WeightKg)
	}
	if v.WidthCm != units.InToCm(10) || v.HeightCm != units.InToCm(15) || v.DepthCm != units.InToCm(15) {
		t.Fatalf("expected dimensions updated to 10x15x15 in, got %.2f x %.2f x %.2f cm", v.WidthCm, v.HeightCm, v.DepthCm)
	}
}

func TestDownloadPackagingTemplateZeroWeightFirstAndRoundedLb(t *testing.T) {
	store := product.NewMockProductStore()
	store.Products["p-heavy"] = &product.Product{ID: "p-heavy", ShopifyID: "shop-heavy", Title: "Heavy Product", Status: "active"}
	store.Products["p-zero"] = &product.Product{ID: "p-zero", ShopifyID: "shop-zero", Title: "Zero Product", Status: "active"}
	store.Variants["gid://shopify/ProductVariant/heavy"] = &product.ProductVariant{
		ProductID:        "p-heavy",
		ShopifyVariantID: "gid://shopify/ProductVariant/heavy",
		Title:            "Heavy",
		SKU:              "HEAVY",
		WeightKg:         existingWeightKg,
		Product:          store.Products["p-heavy"],
	}
	store.Variants["gid://shopify/ProductVariant/zero"] = &product.ProductVariant{
		ProductID:        "p-zero",
		ShopifyVariantID: "gid://shopify/ProductVariant/zero",
		Title:            "Zero",
		SKU:              "ZERO",
		WeightKg:         0,
		Product:          store.Products["p-zero"],
	}

	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	h := product.NewProductHandler(svc, "test-secret")

	app := fiber.New()
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	req := httptest.NewRequest("GET", "/api/v1/products/variants/dimensions/template", nil)
	req.Header.Set("Authorization", "Bearer "+adminTestToken(t))

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "variant-packaging-template.xlsx") {
		t.Fatalf("expected packaging template filename, got %q", cd)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		t.Fatal("expected at least one sheet")
	}
	rows, err := f.GetRows(sheets[0])
	if err != nil {
		t.Fatalf("get rows: %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("expected header + 2 rows, got %d", len(rows))
	}
	if len(rows[0]) < 5 || rows[0][0] != "variant_id" || rows[0][4] != "weight_lb" {
		t.Fatalf("unexpected header: %v", rows[0])
	}
	if rows[1][0] != "gid://shopify/ProductVariant/zero" {
		t.Fatalf("expected zero-weight row first, got %v", rows[1])
	}
	if rows[2][0] != "gid://shopify/ProductVariant/heavy" {
		t.Fatalf("expected heavy row second, got %v", rows[2])
	}
	weightLb := math.Round(units.KgToLb(existingWeightKg)*100) / 100
	expected := fmt.Sprintf("%.2f", weightLb)
	if rows[2][4] != expected {
		t.Fatalf("expected weight_lb %s in heavy row, got %s", expected, rows[2][4])
	}
	width, err := f.GetColWidth(sheets[0], "A")
	if err != nil {
		t.Fatalf("get col width: %v", err)
	}
	if width < 20 {
		t.Fatalf("expected column A to be widened for variant_id, got %.1f", width)
	}
}

func TestSyncFromShopifyPreservesImportedWeight(t *testing.T) {
	store := product.NewMockProductStore()
	store.Products["gid://shopify/Product/1"] = &product.Product{
		ID:        "product-1",
		ShopifyID: "gid://shopify/Product/1",
		Title:     "Rose Mug",
		Status:    "active",
	}
	store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
		ID:               "variant-1",
		ProductID:        "product-1",
		ShopifyVariantID: "gid://shopify/ProductVariant/1",
		Title:            "Default",
		SKU:              "SKU-001",
		WeightKg:         existingWeightKg,
		WidthCm:          10,
		HeightCm:         20,
		DepthCm:          30,
		FulfillmentType:  "ship_ready",
	}

	mockClient := &product.MockShopifyClient{
		AdminGraphQLResponse: buildShopifyProductResponse(
			"gid://shopify/Product/1", "gid://shopify/ProductVariant/1", "Rose Mug", "45.00",
		),
	}
	svc := product.NewProductService(store, mockClient, preordercustomtext.NewMockService())

	if err := svc.SyncFromShopify(context.Background()); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	v := store.Variants["gid://shopify/ProductVariant/1"]
	if v.WeightKg != existingWeightKg {
		t.Fatalf("expected weight_kg preserved at %.2f after sync, got %.2f", existingWeightKg, v.WeightKg)
	}
	if v.WidthCm != 10 || v.HeightCm != 20 || v.DepthCm != 30 {
		t.Fatalf("expected packaging dims preserved, got %.2f x %.2f x %.2f", v.WidthCm, v.HeightCm, v.DepthCm)
	}
}

func postPackagingCSV(t *testing.T, app *fiber.App, csv string) *http.Response {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "packaging.csv")
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
	return resp
}

func decodeSuccessData(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(raw))
	}
	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %v", envelope)
	}
	return data
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
