package dashboard

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

type Handler struct {
	service DashboardService
}

func NewHandler(service DashboardService) *Handler {
	return &Handler{service: service}
}

// GetSummary godoc
// @Summary Get Dashboard Summary
// @Description Returns aggregated stats and data for the operations dashboard
// @Tags Dashboard
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=DashboardSummaryResponse}
// @Router /dashboard/summary [get]
func (h *Handler) GetSummary(c *fiber.Ctx) error {
	summary, err := h.service.GetSummary(c.Context())
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Dashboard summary retrieved successfully", summary)
}

func (h *Handler) SetupRoutes(router fiber.Router, secret string) {
	group := router.Group("/dashboard", middleware.Auth(secret), middleware.RBAC("admin"))
	group.Get("/summary", h.GetSummary)
}
