package product

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	service      ProductService
	batchService BatchManager
	jwtSecret    string
}

type BatchManager interface {
	CancelOpenBatchesForVariant(ctx context.Context, shopifyVariantID string) error
}

func NewProductHandler(service ProductService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetBatchManager(batchService BatchManager) {
	h.batchService = batchService
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
	admin.Post("/custom", h.CreateCustomProduct)
	admin.Patch("/variant/price", h.UpdateVariantPrice)
	admin.Patch("/variant/status", h.UpdateVariantStatus)
	admin.Patch("/variant/ltl", h.UpdateVariantLtl)
	admin.Patch("/variant/batch-label", h.UpdateVariantBatchLabelByVariantID)
	admin.Patch("/variant/link-sku", h.LinkCustomVariantSKU)
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
// @Param status query string false "Filter by Shopify product status (active, unlisted)"
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

// CreateCustomProduct godoc
// @Summary Create a custom UNLISTED Shopify product
// @Tags Product
// @Accept json,multipart/form-data
// @Produce json
// @Security BearerAuth
// @Success 201 {object} response.Envelope{data=ProductDTO}
// @Router /products/custom [post]
func (h *Handler) CreateCustomProduct(c *fiber.Ctx) error {
	var req CreateCustomProductRequest
	var imageBytes *ReferenceImageBytes

	contentType := string(c.Request().Header.ContentType())
	if strings.HasPrefix(contentType, "multipart/form-data") {
		req.Title = strings.TrimSpace(c.FormValue("title"))
		req.IdempotencyKey = strings.TrimSpace(c.FormValue("idempotency_key"))
		if note := strings.TrimSpace(c.FormValue("internal_note")); note != "" {
			req.InternalNote = &note
		}
		if imageURL := strings.TrimSpace(c.FormValue("reference_image_url")); imageURL != "" {
			req.ReferenceImageURL = &imageURL
		}
		variantsJSON := c.FormValue("variants")
		if variantsJSON == "" {
			return response.Error(c, apierror.New(400, "validation_error", "variants is required"))
		}
		if err := json.Unmarshal([]byte(variantsJSON), &req.Variants); err != nil {
			return response.Error(c, apierror.New(400, "validation_error", "variants must be valid JSON"))
		}
		if fileHeader, err := c.FormFile("reference_image"); err == nil && fileHeader != nil {
			file, err := fileHeader.Open()
			if err != nil {
				return response.Error(c, apierror.New(400, "validation_error", "failed to read reference image"))
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil {
				return response.Error(c, apierror.New(400, "validation_error", "failed to read reference image"))
			}
			ct := fileHeader.Header.Get("Content-Type")
			if ct == "" {
				ct = "image/jpeg"
			}
			imageBytes = &ReferenceImageBytes{
				Filename:    fileHeader.Filename,
				ContentType: ct,
				Data:        data,
			}
		}
	} else {
		if err := c.BodyParser(&req); err != nil {
			return response.Error(c, apierror.ErrBadRequest)
		}
		if err := validator.ValidateStruct(&req); err != nil {
			return response.Error(c, err)
		}
	}

	if req.Title == "" || req.IdempotencyKey == "" || len(req.Variants) == 0 {
		return response.Error(c, apierror.New(400, "validation_error", "title, idempotency_key, and variants are required"))
	}

	input := CreateCustomProductInput{
		Title:             req.Title,
		InternalNote:      req.InternalNote,
		IdempotencyKey:    req.IdempotencyKey,
		ReferenceImageURL: req.ReferenceImageURL,
		ReferenceImage:    imageBytes,
	}
	for _, v := range req.Variants {
		input.Variants = append(input.Variants, CreateCustomVariantInput{
			Title:    v.Title,
			RPPPrice: v.RPPPrice,
			WSPrice:  v.WSPrice,
		})
	}

	dto, err := h.service.CreateCustomProduct(c.Context(), input)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusCreated, "Custom product created", dto)
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
	if err := h.cancelOpenVariantBatchesForProductIfNeeded(c.Context(), c.Params("id"), req.FulfillmentType, req.ConfirmBatchCancel); err != nil {
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
	if err := h.cancelOpenVariantBatchesIfNeeded(c.Context(), req.VariantID, req.FulfillmentType, req.ConfirmBatchCancel); err != nil {
		return response.Error(c, err)
	}

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

// UpdateVariantLtl godoc
// @Summary Update variant LTL (less than truckload) flag
// @Tags Admin/Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body UpdateVariantLtlRequest true "Variant LTL Request"
// @Success 200 {object} response.Envelope{data=VariantDTO}
// @Router /products/variant/ltl [patch]
func (h *Handler) UpdateVariantLtl(c *fiber.Ctx) error {
	var req UpdateVariantLtlRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	slog.InfoContext(c.Context(), "UpdateVariantLtl",
		slog.String("variant_id", req.VariantID),
		slog.Bool("is_ltl", req.IsLtl),
	)

	dto, err := h.service.UpdateVariantLtl(c.Context(), req.VariantID, req.IsLtl)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Variant LTL status updated", dto)
}

// LinkCustomVariantSKU godoc
// @Summary Link custom variant SKU to Shopify and enable inventory tracking
// @Tags Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body LinkCustomVariantSKURequest true "Link SKU Request"
// @Success 200 {object} response.Envelope{data=VariantDTO}
// @Router /products/variant/link-sku [patch]
func (h *Handler) LinkCustomVariantSKU(c *fiber.Ctx) error {
	var req LinkCustomVariantSKURequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	slog.InfoContext(c.Context(), "LinkCustomVariantSKU",
		slog.String("variant_id", req.VariantID),
		slog.String("sku", req.SKU),
	)

	dto, err := h.service.LinkCustomVariantSKU(c.Context(), req.VariantID, req.SKU)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Variant linked to Shopify", dto)
}

func (h *Handler) cancelOpenVariantBatchesForProductIfNeeded(ctx context.Context, productID string, nextFulfillmentType string, confirm bool) error {
	variants, err := h.service.GetVariantsByProductID(ctx, productID)
	if err != nil {
		return err
	}
	for _, variant := range variants {
		if err := h.cancelOpenVariantBatchesIfNeeded(ctx, variant.ID, nextFulfillmentType, confirm); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) cancelOpenVariantBatchesIfNeeded(ctx context.Context, variantID string, nextFulfillmentType string, confirm bool) error {
	if h.batchService == nil {
		return nil
	}
	if nextFulfillmentType == string(FulfillmentTypePreOrder) {
		return nil
	}

	variant, err := h.service.GetVariantByID(ctx, variantID)
	if err != nil {
		return err
	}
	if variant.FulfillmentType != FulfillmentTypePreOrder {
		return nil
	}
	if variant.BatchSummary == nil || (variant.BatchSummary.ActiveCount == 0 && variant.BatchSummary.QueuedCount == 0) {
		return nil
	}
	if !confirm {
		return apierror.New(fiber.StatusConflict, "batch_cancel_confirmation_required", "Changing this variant status will cancel active and queued pre-order batches")
	}
	return h.batchService.CancelOpenBatchesForVariant(ctx, variantID)
}

// DownloadDimensionTemplate godoc
// @Summary Download variant packaging Excel template (weight + dimensions)
// @Tags Admin/Product
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security BearerAuth
// @Router /products/variants/dimensions/template [get]
func (h *Handler) DownloadDimensionTemplate(c *fiber.Ctx) error {
	variants, err := h.service.GetAllVariants(c.Context())
	if err != nil {
		return response.Error(c, err)
	}

	sort.SliceStable(variants, func(i, j int) bool {
		zi := variants[i].WeightKg == 0
		zj := variants[j].WeightKg == 0
		if zi != zj {
			return zi // zero-weight rows first
		}
		pi, pj := "", ""
		if variants[i].Product != nil {
			pi = variants[i].Product.Title
		}
		if variants[j].Product != nil {
			pj = variants[j].Product.Title
		}
		if pi != pj {
			return pi < pj
		}
		if variants[i].SKU != variants[j].SKU {
			return variants[i].SKU < variants[j].SKU
		}
		return variants[i].ShopifyVariantID < variants[j].ShopifyVariantID
	})

	excelBytes, err := buildPackagingTemplateExcel(variants)
	if err != nil {
		return response.Error(c, apierror.New(500, "internal_error", "Failed to build packaging template"))
	}

	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", `attachment; filename="variant-packaging-template.xlsx"`)
	return c.Send(excelBytes)
}

// ImportDimensions godoc
// @Summary Import variant packaging (weight lb + dimensions) via Excel or CSV
// @Tags Admin/Product
// @Accept mpfd
// @Produce json
// @Security BearerAuth
// @Param file formData file true "Excel (.xlsx) or CSV File"
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

	records, err := readPackagingImportRecords(fileHeader.Filename, file)
	if err != nil {
		return response.Error(c, apierror.New(400, "bad_request", err.Error()))
	}

	if len(records) < 2 {
		return response.Error(c, apierror.New(400, "bad_request", "File is empty"))
	}

	headers := records[0]
	idxMap := make(map[string]int)
	for i, h := range headers {
		idxMap[strings.TrimSpace(h)] = i
	}

	vidIdx, ok := idxMap["variant_id"]
	if !ok {
		return response.Error(c, apierror.New(400, "bad_request", "Missing variant_id column"))
	}

	cell := func(row []string, col string) (string, bool) {
		idx, found := idxMap[col]
		if !found || len(row) <= idx {
			return "", false
		}
		return strings.TrimSpace(row[idx]), true
	}

	parseDimInchesOrCm := func(row []string, inCol, cmCol string) (float64, bool) {
		if raw, ok := cell(row, inCol); ok && raw != "" {
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				return units.InToCm(f), true
			}
			return 0, true
		}
		if raw, ok := cell(row, cmCol); ok && raw != "" {
			if f, err := strconv.ParseFloat(raw, 64); err == nil {
				return f, true
			}
			return 0, true
		}
		return 0, false
	}

	type pendingRow struct {
		csvRow int
		input  DimensionUpdateInput
	}
	var pending []pendingRow
	importErrors := make([]PackagingImportError, 0)

	for rowNum, row := range records[1:] {
		csvRow := rowNum + 2
		if len(row) <= vidIdx {
			continue
		}
		vid := strings.TrimSpace(row[vidIdx])
		if vid == "" {
			continue
		}

		input := DimensionUpdateInput{ShopifyVariantID: vid}
		hasDimCol := false

		if v, ok := parseDimInchesOrCm(row, "width_in", "width_cm"); ok {
			input.WidthCm = v
			hasDimCol = true
		}
		if v, ok := parseDimInchesOrCm(row, "height_in", "height_cm"); ok {
			input.HeightCm = v
			hasDimCol = true
		}
		if v, ok := parseDimInchesOrCm(row, "length_in", "length_cm"); ok {
			input.DepthCm = v
			hasDimCol = true
		} else if v, ok := parseDimInchesOrCm(row, "depth_in", "depth_cm"); ok {
			input.DepthCm = v
			hasDimCol = true
		}

		if raw, hasCol := cell(row, "weight_lb"); hasCol && raw != "" {
			lb, err := strconv.ParseFloat(raw, 64)
			if err != nil || lb <= 0 {
				importErrors = append(importErrors, PackagingImportError{
					Row: csvRow, VariantID: vid, Field: "weight_lb",
					Message: "must be a number > 0",
				})
			} else {
				kg := units.LbToKg(lb)
				input.WeightKg = &kg
			}
		}

		if !hasDimCol && input.WeightKg == nil {
			continue
		}

		if hasDimCol && input.WidthCm == 0 && input.HeightCm == 0 && input.DepthCm == 0 {
			slog.Warn("packaging import: all dimensions are zero",
				slog.String("variant_id", vid),
			)
		}

		input.UpdateDimensions = hasDimCol
		pending = append(pending, pendingRow{csvRow: csvRow, input: input})
	}

	updates := make([]DimensionUpdateInput, len(pending))
	for i, p := range pending {
		updates[i] = p.input
	}

	result, err := h.service.BulkUpdateDimensions(c.Context(), updates)
	if err != nil {
		return response.Error(c, err)
	}

	notFoundSet := make(map[string]struct{}, len(result.NotFoundIDs))
	for _, id := range result.NotFoundIDs {
		notFoundSet[id] = struct{}{}
	}
	for _, p := range pending {
		if _, missing := notFoundSet[p.input.ShopifyVariantID]; missing {
			importErrors = append(importErrors, PackagingImportError{
				Row:       p.csvRow,
				VariantID: p.input.ShopifyVariantID,
				Message:   "variant_id not found",
			})
		}
	}

	return response.Success(c, fiber.StatusOK, "Packaging data imported successfully", map[string]interface{}{
		"updated_count":        result.UpdatedCount,
		"weight_updated_count": result.WeightUpdatedCount,
		"errors":               importErrors,
	})
}
