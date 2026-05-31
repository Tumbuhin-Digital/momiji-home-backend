package preorder

import (
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
	group.Get("/settlements/:id", h.GetSettlement)
	group.Patch("/settlements/:id/invoice", h.InvoiceSettlement)
	group.Patch("/settlements/:id/paid", h.MarkSettlementPaid)
}

// ListSettlements godoc
// @Summary List pre-order settlements
// @Tags Preorder
// @Produce json
// @Security BearerAuth
// @Param status query string false "Filter by status (pending|invoiced|paid)"
// @Param page   query int    false "Page number (default 1)"
// @Param limit  query int    false "Items per page (default 20)"
// @Success 200 {object} response.Envelope{data=[]SettlementResponse}
// @Router /preorders [get]
func (h *Handler) ListSettlements(c *fiber.Ctx) error {
	var q ListSettlementsQuery
	if err := c.QueryParser(&q); err != nil {
		return response.Error(c, err)
	}

	filter := SettlementFilter{
		Status:     q.Status,
		BatchLabel: q.BatchLabel,
		Page:       q.Page,
		Limit:      q.Limit,
	}

	settlements, total, err := h.service.ListSettlements(c.Context(), filter)
	if err != nil {
		return response.Error(c, err)
	}

	limit := filter.Limit
	if limit < 1 { limit = 20 }
	page := filter.Page
	if page < 1 { page = 1 }
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

// InvoiceSettlement godoc
// @Summary Transition settlement: pending → invoiced
// @Tags Preorder
// @Produce json
// @Security BearerAuth
// @Param id path string true "Settlement ID"
// @Success 200 {object} response.Envelope{data=SettlementResponse}
// @Failure 409 {object} response.Envelope
// @Router /preorders/settlements/{id}/invoice [patch]
func (h *Handler) InvoiceSettlement(c *fiber.Ctx) error {
	id := c.Params("id")
	st, err := h.service.InvoiceSettlement(c.Context(), id)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Settlement invoiced", st)
}

// MarkSettlementPaid godoc
// @Summary Transition settlement: invoiced → paid
// @Tags Preorder
// @Produce json
// @Security BearerAuth
// @Param id path string true "Settlement ID"
// @Success 200 {object} response.Envelope{data=SettlementResponse}
// @Failure 409 {object} response.Envelope
// @Router /preorders/settlements/{id}/paid [patch]
func (h *Handler) MarkSettlementPaid(c *fiber.Ctx) error {
	id := c.Params("id")
	st, err := h.service.MarkSettlementPaid(c.Context(), id)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Settlement marked as paid", st)
}
