package product

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
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
	group.Get("/catalog", h.GetCatalogProducts)
	group.Get("/:id/variants", h.GetProductVariants)
	group.Get("/:id", h.GetProductByID)

	// Admin endpoints
	admin := group.Group("/")
	admin.Use(middleware.Auth(h.jwtSecret))
	admin.Use(middleware.RBAC("admin"))
	admin.Post("/sync", h.SyncProducts)
	admin.Patch("/variant/price", h.UpdateVariantPrice)
	admin.Patch("/variant/status", h.UpdateVariantStatus)
	admin.Patch("/variant/batch-label", h.UpdateVariantBatchLabelByVariantID)
	admin.Patch("/:id/status", h.UpdateProductStatus)
	admin.Patch("/:id/batch", h.UpdateVariantBatchLabel)
	admin.Get("/variants/dimensions/template", h.DownloadDimensionTemplate)
	admin.Post("/variants/dimensions/import", h.ImportDimensions)
}

// GetProducts godoc
// @Summary List products
// @Tags Product
// @Produce json
// @Param page query int false "Page number"
// @Param limit query int false "Limit per page"
// @Param search query string false "Search by title or description"
// @Param sort query string false "Sort order (e.g. price_asc, price_desc, name_asc, created_at, stock_asc, stock_desc)"
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

// GetCatalogProducts godoc
// @Summary List catalog products excluding inactive ones
// @Tags Product
// @Produce json
// @Param page query int false "Page number"
// @Param limit query int false "Limit per page"
// @Param search query string false "Search by title or description"
// @Param sort query string false "Sort order (e.g. price_asc, price_desc, name_asc, created_at, stock_asc, stock_desc)"
// @Param fulfillment_type query string false "Filter by fulfillment type (ship_ready, pre_order)"
// @Success 200 {object} response.Envelope{data=response.PaginatedData}
// @Router /products/catalog [get]
func (h *Handler) GetCatalogProducts(c *fiber.Ctx) error {
	var query ProductQuery
	if err := c.QueryParser(&query); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}

	query.ExcludeInactive = true
	query.FilterVariantsInResponse = true

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

	return response.Success(c, fiber.StatusOK, "Catalog products retrieved", paginatedData)
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
// @Param body body UpdateProductBatchLabelRequest true "Batch label and ship date update payload"
// @Success 200 {object} response.Envelope{data=map[string]interface{}}
// @Router /products/{id}/batch [patch]
func (h *Handler) UpdateVariantBatchLabel(c *fiber.Ctx) error {
	slog.InfoContext(c.Context(), "UpdateVariantBatchLabel", slog.String("product_id", c.Params("id")))
	var req UpdateProductBatchLabelRequest
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

// UpdateVariantStatus godoc
// @Summary Update variant fulfillment type
// @Tags Admin/Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body UpdateVariantStatusRequest true "Variant Status Request"
// @Success 200 {object} response.Envelope{data=VariantDTO}
// @Router /products/variant/status [patch]
func (h *Handler) UpdateVariantStatus(c *fiber.Ctx) error {
	var req UpdateVariantStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	slog.InfoContext(c.Context(), "UpdateVariantStatus", slog.String("variant_id", req.VariantID))

	dto, err := h.service.UpdateVariantStatus(c.Context(), req.VariantID, req.FulfillmentType)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Variant status updated", dto)
}

// UpdateVariantBatchLabelByVariantID godoc
// @Summary Update preorder custom text for a single variant
// @Tags Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body UpdateVariantBatchLabelRequest true "Variant batch label update payload"
// @Success 200 {object} response.Envelope{data=VariantDTO}
// @Router /products/variant/batch-label [patch]
func (h *Handler) UpdateVariantBatchLabelByVariantID(c *fiber.Ctx) error {
	var req UpdateVariantBatchLabelRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	dto, err := h.service.UpdateVariantBatchLabelByVariantID(c.Context(), req.VariantID, req.PreorderBatchLabel)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Variant batch label updated", dto)
}

// UpdateVariantPrice godoc
// @Summary Update variant wholesale/retail price
// @Tags Admin/Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body UpdateVariantPriceRequest true "Variant Price Request"
// @Success 200 {object} response.Envelope
// @Router /products/variant/price [patch]
func (h *Handler) UpdateVariantPrice(c *fiber.Ctx) error {
	var req UpdateVariantPriceRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	slog.InfoContext(c.Context(), "UpdateVariantPrice", slog.String("variant_id", req.VariantID))

	if err := h.service.UpdateVariantPrice(c.Context(), req.VariantID, req.WSPrice); err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Variant price updated", nil)
}

// DownloadDimensionTemplate godoc
// @Summary Download product dimension CSV template
// @Tags Admin/Product
// @Produce text/csv
// @Security BearerAuth
// @Router /products/variants/dimensions/template [get]
func (h *Handler) DownloadDimensionTemplate(c *fiber.Ctx) error {
	variants, err := h.service.GetAllVariants(c.Context())
	if err != nil {
		return response.Error(c, err)
	}

	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", `attachment; filename="dimension-template.csv"`)

	writer := csv.NewWriter(c.Response().BodyWriter())
	header := []string{"variant_id", "product_title", "variant_title", "sku", "length_in", "width_in", "height_in"}
	_ = writer.Write(header)

	for _, v := range variants {
		productTitle := ""
		if v.Product != nil {
			productTitle = v.Product.Title
		}
		row := []string{
			v.ShopifyVariantID,
			productTitle,
			v.Title,
			v.SKU,
			fmt.Sprintf("%.2f", units.CmToIn(v.DepthCm)),
			fmt.Sprintf("%.2f", units.CmToIn(v.WidthCm)),
			fmt.Sprintf("%.2f", units.CmToIn(v.HeightCm)),
		}
		_ = writer.Write(row)
	}
	writer.Flush()
	return nil
}

// ImportDimensions godoc
// @Summary Import product dimensions via CSV
// @Tags Admin/Product
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Param file formData file true "CSV File"
// @Success 200 {object} response.Envelope
// @Router /products/variants/dimensions/import [post]
func (h *Handler) ImportDimensions(c *fiber.Ctx) error {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return response.Error(c, apierror.New(400, "bad_request", "Missing file parameter"))
	}

	file, err := fileHeader.Open()
	if err != nil {
		return response.Error(c, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return response.Error(c, apierror.New(400, "bad_request", "Invalid CSV format"))
	}

	if len(records) < 2 {
		return response.Error(c, apierror.New(400, "bad_request", "CSV is empty"))
	}

	var updates []DimensionUpdateInput
	// Map header indices
	headers := records[0]
	idxMap := make(map[string]int)
	for i, h := range headers {
		idxMap[h] = i
	}

	vidIdx, ok := idxMap["variant_id"]
	if !ok {
		return response.Error(c, apierror.New(400, "bad_request", "Missing variant_id column"))
	}

	parseFloat := func(s string) float64 {
		if s == "" {
			return 0
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}

	for _, row := range records[1:] {
		if len(row) <= vidIdx {
			continue
		}
		vid := row[vidIdx]
		if vid == "" {
			continue
		}

		input := DimensionUpdateInput{ShopifyVariantID: vid}
		if idx, ok := idxMap["width_in"]; ok && len(row) > idx && row[idx] != "" {
			input.WidthCm = units.InToCm(parseFloat(row[idx]))
		} else if idx, ok := idxMap["width_cm"]; ok && len(row) > idx && row[idx] != "" {
			input.WidthCm = parseFloat(row[idx])
		}
		if idx, ok := idxMap["height_in"]; ok && len(row) > idx && row[idx] != "" {
			input.HeightCm = units.InToCm(parseFloat(row[idx]))
		} else if idx, ok := idxMap["height_cm"]; ok && len(row) > idx && row[idx] != "" {
			input.HeightCm = parseFloat(row[idx])
		}
		if idx, ok := idxMap["length_in"]; ok && len(row) > idx && row[idx] != "" {
			input.DepthCm = units.InToCm(parseFloat(row[idx]))
		} else if idx, ok := idxMap["length_cm"]; ok && len(row) > idx && row[idx] != "" {
			input.DepthCm = parseFloat(row[idx])
		} else if idx, ok := idxMap["depth_in"]; ok && len(row) > idx && row[idx] != "" {
			input.DepthCm = units.InToCm(parseFloat(row[idx]))
		} else if idx, ok := idxMap["depth_cm"]; ok && len(row) > idx && row[idx] != "" {
			input.DepthCm = parseFloat(row[idx])
		}
		if input.WidthCm == 0 && input.HeightCm == 0 && input.DepthCm == 0 {
			slog.Warn("dimension import: all dimensions are zero",
				slog.String("variant_id", vid),
			)
		}
		updates = append(updates, input)
	}

	if err := h.service.BulkUpdateDimensions(c.Context(), updates); err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Dimensions imported successfully", map[string]interface{}{
		"updated_count": len(updates),
	})
}
