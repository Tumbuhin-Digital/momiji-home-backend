package product

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

type Handler struct {
	service   ProductService
	jwtSecret string
}

func NewProductHandler(service ProductService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/products")

	// Public
	group.Get("/", h.GetProducts)

	// Admin
	admin := group.Group("/")
	admin.Use(middleware.Auth(h.jwtSecret))
	// TODO: Add admin role check middleware
	admin.Post("/sync", h.SyncProducts)
}

// GetProducts godoc
// @Summary List products
// @Tags Product
// @Produce json
// @Success 200 {object} response.Envelope{data=[]VariantDTO}
// @Router /products [get]
func (h *Handler) GetProducts(c *fiber.Ctx) error {
	variants, err := h.service.GetVariants(c.Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Products retrieved", variants)
}

// SyncProducts godoc
// @Summary Sync products from Shopify
// @Tags Product
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope
// @Router /products/sync [post]
func (h *Handler) SyncProducts(c *fiber.Ctx) error {
	// Role check could go here
	if err := h.service.SyncFromShopify(c.Context()); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Products synced successfully", nil)
}
