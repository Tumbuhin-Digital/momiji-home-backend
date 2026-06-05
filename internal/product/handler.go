package product

import (
	"log/slog"
	"net/url"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
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

	// Public endpoints
	group.Get("/", h.GetProducts)
	group.Get("/:id/variants", h.GetProductVariants)
	group.Get("/:id", h.GetProductByID)

	// Admin endpoints
	admin := group.Group("/")
	admin.Use(middleware.Auth(h.jwtSecret))
	admin.Use(middleware.RBAC("admin"))
	admin.Post("/sync", h.SyncProducts)
	admin.Patch("/variant/:id/price", h.UpdateVariantPrice)
	admin.Patch("/:id/status", h.UpdateProductStatus)
	admin.Patch("/:id/batch", h.UpdateVariantBatchLabel)
}

// GetProducts godoc
// @Summary List products
// @Tags Product
// @Produce json
// @Param page query int false "Page number"
// @Param limit query int false "Limit per page"
// @Param search query string false "Search by title or description"
// @Param sort query string false "Sort order (e.g. price_asc, price_desc, name_asc, created_at)"
// @Param fulfillment_type query string false "Filter by fulfillment type (ship_ready, pre_order)"
// @Success 200 {object} response.Envelope{data=response.PaginatedData}
// @Router /products [get]
func (h *Handler) GetProducts(c *fiber.Ctx) error {
	var query ProductQuery
	if err := c.QueryParser(&query); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}

	products, total, err := h.service.GetProducts(c.Context(), query)
	if err != nil {
		return response.Error(c, err)
	}

	limit := query.Limit
	if limit < 1 {
		limit = 20
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))

	paginatedData := response.PaginatedData{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		ItemsKey:   "products",
		Items:      products,
		Extra: map[string]interface{}{
			"sort":   query.Sort,
			"search": query.Search,
		},
	}

	return response.Success(c, fiber.StatusOK, "Products retrieved", paginatedData)
}

// SyncProducts godoc
// @Summary Sync products from Shopify
// @Tags Product
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope
// @Router /products/sync [post]
func (h *Handler) SyncProducts(c *fiber.Ctx) error {
	if err := h.service.SyncFromShopify(c.Context()); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Products synced successfully", nil)
}

// GetProductByID godoc
// @Summary Get product by ID
// @Tags Product
// @Produce json
// @Param id path string true "Product ID"
// @Success 200 {object} response.Envelope{data=ProductDTO}
// @Failure 404 {object} response.Envelope{error=response.ErrorBlock}
// @Router /products/{id} [get]
func (h *Handler) GetProductByID(c *fiber.Ctx) error {
	slog.InfoContext(c.Context(), "GetProductByID", slog.String("product_id", c.Params("id")))
	dto, err := h.service.GetProductByID(c.Context(), c.Params("id"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Product retrieved", dto)
}

// GetProductVariants godoc
// @Summary Get variants for a product
// @Tags Product
// @Produce json
// @Param id path string true "Product ID"
// @Success 200 {object} response.Envelope{data=[]VariantDTO}
// @Router /products/{id}/variants [get]
func (h *Handler) GetProductVariants(c *fiber.Ctx) error {
	slog.InfoContext(c.Context(), "GetProductVariants", slog.String("product_id", c.Params("id")))
	variants, err := h.service.GetVariantsByProductID(c.Context(), c.Params("id"))
	if err != nil {
		return response.Error(c, err)
	}

	data := map[string]interface{}{
		"product_id": c.Params("id"),
		"variants":   variants,
	}

	return response.Success(c, fiber.StatusOK, "Variants retrieved", data)
}

// UpdateProductStatus godoc
// @Summary Update product fulfillment type
// @Tags Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param body body UpdateProductStatusRequest true "Fulfillment type update payload"
// @Success 200 {object} response.Envelope{data=map[string]interface{}}
// @Failure 400 {object} response.Envelope{error=response.ErrorBlock}
// @Router /products/{id}/status [patch]
func (h *Handler) UpdateProductStatus(c *fiber.Ctx) error {
	slog.InfoContext(c.Context(), "UpdateProductStatus", slog.String("product_id", c.Params("id")))
	var req UpdateProductStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	p, err := h.service.UpdateProductStatus(c.Context(), c.Params("id"), req.FulfillmentType)
	if err != nil {
		return response.Error(c, err)
	}

	// Assuming all variants have same fulfillment_type, label, and date after update
	var batchLabel, shipDate *string
	var fulfillmentType string
	if p != nil && len(p.Variants) > 0 {
		fulfillmentType = string(p.Variants[0].FulfillmentType)
		batchLabel = p.PreorderBatchLabel
		shipDate = p.ExpectedShipDate
	}

	data := map[string]interface{}{
		"id":                   c.Params("id"),
		"fulfillment_type":     fulfillmentType,
		"preorder_batch_label": batchLabel,
		"expected_ship_date":   shipDate,
	}

	return response.Success(c, fiber.StatusOK, "Product status updated", data)
}

// UpdateVariantBatchLabel godoc
// @Summary Update preorder batch label for all variants of a product
// @Tags Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Param body body UpdateVariantBatchLabelRequest true "Batch label and ship date update payload"
// @Success 200 {object} response.Envelope{data=map[string]interface{}}
// @Router /products/{id}/batch [patch]
func (h *Handler) UpdateVariantBatchLabel(c *fiber.Ctx) error {
	slog.InfoContext(c.Context(), "UpdateVariantBatchLabel", slog.String("product_id", c.Params("id")))
	var req UpdateVariantBatchLabelRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	p, err := h.service.UpdateVariantBatchLabel(c.Context(), c.Params("id"), req.PreorderBatchLabel, req.ExpectedShipDate)
	if err != nil {
		return response.Error(c, err)
	}

	var batchLabel, shipDate *string
	var fulfillmentType string
	if p != nil && len(p.Variants) > 0 {
		fulfillmentType = string(p.Variants[0].FulfillmentType)
		batchLabel = p.PreorderBatchLabel
		shipDate = p.ExpectedShipDate
	}

	data := map[string]interface{}{
		"id":                   c.Params("id"),
		"fulfillment_type":     fulfillmentType,
		"preorder_batch_label": batchLabel,
		"expected_ship_date":   shipDate,
	}

	return response.Success(c, fiber.StatusOK, "Batch label updated", data)
}

// UpdateVariantPrice godoc
// @Summary Override ws_price and/or retail_price for a variant
// @Tags Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Variant ID"
// @Param body body UpdateVariantPriceRequest true "Price override payload"
// @Router /products/variant/{id}/price [patch]
func (h *Handler) UpdateVariantPrice(c *fiber.Ctx) error {
	variantID, _ := url.PathUnescape(c.Params("id"))
	slog.InfoContext(c.Context(), "UpdateVariantPrice", slog.String("variant_id", variantID))
	var req UpdateVariantPriceRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := h.service.UpdateVariantPrice(c.Context(), variantID, req.WSPrice, req.RetailPrice); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Variant price updated", nil)
}
