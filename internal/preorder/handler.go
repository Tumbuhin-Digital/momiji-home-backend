package preorder

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

// Handler exposes pre-order settlement endpoints.
// All routes require admin role.
type Handler struct {
	service   PreorderService
	jwtSecret string
}

func NewPreorderHandler(service PreorderService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/preorders",
		middleware.Auth(h.jwtSecret),
		middleware.RBAC("admin"),
	)

	group.Get("/", h.ListSettlements)
	group.Get("/export", h.ExportPreorders)
	group.Get("/settlements/:id", h.GetSettlement)
	group.Patch("/invoice", h.InvoiceSettlements)
	group.Patch("/paid", h.MarkSettlementsPaid)
}

// ListSettlements godoc
// @Summary List pre-order settlements
// @Tags Preorder
// @Produce json
// @Security BearerAuth
// @Param status     query string false "Filter by status (pending|invoiced|paid)"
// @Param start_date query string false "Filter from date (YYYY-MM-DD, inclusive)"
// @Param end_date   query string false "Filter to date (YYYY-MM-DD, inclusive)"
// @Param page       query int    false "Page number (default 1)"
// @Param limit      query int    false "Items per page (default 20)"
// @Success 200 {object} response.Envelope{data=response.PaginatedData{preorders=[]PreorderGroupResponse}}
// @Router /preorders [get]
func (h *Handler) ListSettlements(c *fiber.Ctx) error {
	var q ListSettlementsQuery
	if err := c.QueryParser(&q); err != nil {
		return response.Error(c, err)
	}

	filter, err := buildSettlementFilter(q)
	if err != nil {
		return response.Error(c, err)
	}

	settlements, total, err := h.service.ListSettlements(c.Context(), filter)
	if err != nil {
		return response.Error(c, err)
	}

	limit := filter.Limit
	if limit < 1 {
		limit = 20
	}
	page := filter.Page
	if page < 1 {
		page = 1
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))

	paginatedData := response.PaginatedData{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		ItemsKey:   "preorders",
		Items:      settlements,
	}

	return response.Success(c, fiber.StatusOK, "Settlements retrieved", paginatedData)
}

// GetSettlement godoc
// @Summary Get a single settlement by ID
// @Tags Preorder
// @Produce json
// @Security BearerAuth
// @Param id path string true "Settlement ID"
// @Success 200 {object} response.Envelope{data=SettlementResponse}
// @Router /preorders/settlements/{id} [get]
func (h *Handler) GetSettlement(c *fiber.Ctx) error {
	id := c.Params("id")
	st, err := h.service.GetSettlement(c.Context(), id)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Settlement retrieved", st)
}

// InvoiceSettlements godoc
// @Summary Transition multiple settlements: pending → invoiced
// @Tags Preorder
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body BulkSettlementRequest true "Bulk Invoice Request"
// @Success 200 {object} response.Envelope{data=[]SettlementResponse}
// @Failure 409 {object} response.Envelope
// @Router /preorders/invoice [patch]
func (h *Handler) InvoiceSettlements(c *fiber.Ctx) error {
	var req BulkSettlementRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}

	sts, err := h.service.InvoiceSettlements(c.Context(), req.OrderLineItemIDs)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Settlements invoiced", sts)
}

// MarkSettlementsPaid godoc
// @Summary Transition multiple settlements: invoiced → paid
// @Tags Preorder
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body BulkSettlementRequest true "Bulk Paid Request"
// @Success 200 {object} response.Envelope{data=[]SettlementResponse}
// @Failure 409 {object} response.Envelope
// @Router /preorders/paid [patch]
func (h *Handler) MarkSettlementsPaid(c *fiber.Ctx) error {
	var req BulkSettlementRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}

	sts, err := h.service.MarkSettlementsPaid(c.Context(), req.OrderLineItemIDs)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Settlements marked as paid", sts)
}

// ExportPreorders godoc
// @Summary Export preorder list to Excel (Admin only)
// @Tags Preorder
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security BearerAuth
// @Param status      query string false "Filter by status"
// @Param batch_label query string false "Filter by batch label"
// @Param start_date  query string false "Filter from date (YYYY-MM-DD, inclusive)"
// @Param end_date    query string false "Filter to date (YYYY-MM-DD, inclusive)"
// @Success 200 {file} file "preorder_list.xlsx"
// @Router /preorders/export [get]
func (h *Handler) ExportPreorders(c *fiber.Ctx) error {
	var q ListSettlementsQuery
	if err := c.QueryParser(&q); err != nil {
		return response.Error(c, err)
	}

	filter, err := buildSettlementFilter(q)
	if err != nil {
		return response.Error(c, err)
	}

	excelBytes, err := h.service.ExportPreordersToExcel(c.Context(), filter)
	if err != nil {
		return response.Error(c, err)
	}

	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", `attachment; filename="preorder_list.xlsx"`)
	return c.Send(excelBytes)
}

func buildSettlementFilter(q ListSettlementsQuery) (SettlementFilter, error) {
	filter := SettlementFilter{
		Status:     q.Status,
		BatchLabel: q.BatchLabel,
		Page:       q.Page,
		Limit:      q.Limit,
	}

	if q.StartDate != "" {
		t, err := time.Parse("2006-01-02", q.StartDate)
		if err != nil {
			return filter, fiber.NewError(fiber.StatusBadRequest, "invalid start_date format, expected YYYY-MM-DD")
		}
		startOfDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		filter.StartDate = &startOfDay
	}

	if q.EndDate != "" {
		t, err := time.Parse("2006-01-02", q.EndDate)
		if err != nil {
			return filter, fiber.NewError(fiber.StatusBadRequest, "invalid end_date format, expected YYYY-MM-DD")
		}
		startOfDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		filter.EndDate = &startOfDay
	}

	return filter, nil
}
