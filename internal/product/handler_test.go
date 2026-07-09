package product_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preordercustomtext"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
)



func TestGetProductByIDHandler_NotFound(t *testing.T) {
	app := fiber.New()
	store := product.NewMockProductStore()
	svc := product.NewProductService(store, &product.MockShopifyClient{}, preordercustomtext.NewMockService())
	h := product.NewProductHandler(svc, "test-secret")
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	req := httptest.NewRequest("GET", "/api/v1/products/nonexistent-id", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
