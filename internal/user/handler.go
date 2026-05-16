package user

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	service UserService
	jwtSecret string
}

func NewUserHandler(service UserService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/users")
	
	// Apply Auth middleware
	group.Use(middleware.Auth(h.jwtSecret))

	group.Get("/me", h.GetMe)
	group.Patch("/me", h.UpdateMe)
}

// GetMe godoc
// @Summary Get current user
// @Description Get current user profile
// @Tags User
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=UserResponse}
// @Failure 401 {object} response.Envelope{error=response.ErrorBlock}
// @Router /users/me [get]
func (h *Handler) GetMe(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	res, err := h.service.GetMe(c.Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, res, nil)
}

// UpdateMe godoc
// @Summary Update current user
// @Description Update current user profile
// @Tags User
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body UpdateUserRequest true "Update User Request"
// @Success 200 {object} response.Envelope{data=UserResponse}
// @Failure 400 {object} response.Envelope{error=response.ErrorBlock}
// @Failure 401 {object} response.Envelope{error=response.ErrorBlock}
// @Router /users/me [patch]
func (h *Handler) UpdateMe(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	var req UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}

	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	res, err := h.service.UpdateMe(c.Context(), userID, req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, res, nil)
}
