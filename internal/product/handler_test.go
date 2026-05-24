package product_test

import (
	"io"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
)

func TestSyncEndpoint_CustomerRoleReturns403(t *testing.T) {
	app := fiber.New()
	store := product.NewMockProductStore()
	svc := product.NewProductService(store, &product.MockShopifyClient{})
	h := product.NewProductHandler(svc, "test-secret")

	// Mount handler
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	// Simulate a customer-role JWT
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "test-user")
		c.Locals("role", "customer") // Not admin
		return c.Next()
	})

	req := httptest.NewRequest("POST", "/api/v1/products/sync", nil)
	resp, _ := app.Test(req)
	defer resp.Body.Close()

	if resp.StatusCode != 403 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}
}

func TestGetProductByIDHandler_NotFound(t *testing.T) {
	app := fiber.New()
	store := product.NewMockProductStore()
	svc := product.NewProductService(store, &product.MockShopifyClient{})
	h := product.NewProductHandler(svc, "test-secret")
	api := app.Group("/api/v1")
	h.SetupRoutes(api)

	req := httptest.NewRequest("GET", "/api/v1/products/nonexistent-id", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
