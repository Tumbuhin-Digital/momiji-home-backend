package settings

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

type Handler struct {
	service   SettingsService
	jwtSecret string
}

func NewSettingsHandler(service SettingsService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/settings")

	group.Get("/checkout-notes", h.GetCheckoutNotes)

	admin := group.Group("/")
	admin.Use(middleware.Auth(h.jwtSecret))
	admin.Use(middleware.RBAC("admin"))
	admin.Get("/", h.GetSettings)
	admin.Put("/", h.UpdateSettings)
}

// GetCheckoutNotes godoc
// @Summary Get checkout shipping notes
// @Description Returns editable shipping description copy for checkout
// @Tags Settings
// @Produce json
// @Success 200 {object} response.Envelope{data=CheckoutNotesResponse}
// @Router /settings/checkout-notes [get]
func (h *Handler) GetCheckoutNotes(c *fiber.Ctx) error {
	notes, err := h.service.GetCheckoutNotes(c.Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Checkout notes retrieved", notes)
}

// GetSettings godoc
// @Summary Get app settings
// @Description Returns current checkout note settings for admin dashboard
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=CheckoutNotesResponse}
// @Router /settings [get]
func (h *Handler) GetSettings(c *fiber.Ctx) error {
	notes, err := h.service.GetCheckoutNotes(c.Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Settings retrieved", notes)
}

// UpdateSettings godoc
// @Summary Update app settings
// @Description Updates checkout note settings
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body UpdateSettingsRequest true "Settings payload"
// @Success 200 {object} response.Envelope{data=CheckoutNotesResponse}
// @Router /settings [put]
func (h *Handler) UpdateSettings(c *fiber.Ctx) error {
	var req UpdateSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}

	notes, err := h.service.UpdateCheckoutNotes(c.Context(), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Settings updated successfully", notes)
}
