package auth

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	service   AuthService
	jwtSecret string
}

func NewAuthHandler(service AuthService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/auth")

	rateLimit := middleware.RateLimit(5, 15*time.Minute)

	// DEV ONLY — remove before production
	group.Post("/register", rateLimit, h.Register)

	group.Post("/login", rateLimit, h.Login)
	group.Post("/refresh", h.Refresh)

	// Protected routes
	protected := group.Group("/")
	protected.Use(middleware.Auth(h.jwtSecret))
	protected.Post("/logout", h.Logout)
	protected.Get("/me", h.GetMe)
}

// Register godoc
// @Summary Register user (DEV ONLY)
// @Description Temporary endpoint for FE development. Creates a new customer account.
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Register Request"
// @Success 201 {object} response.Envelope{data=TokenResponse}
// @Failure 400 {object} response.Envelope{error=response.ErrorBlock}
// @Failure 409 {object} response.Envelope{error=response.ErrorBlock}
// @Router /auth/register [post]
func (h *Handler) Register(c *fiber.Ctx) error {
	var req RegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}

	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	res, err := h.service.Register(c.Context(), req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusCreated, "User registered successfully", res)
}

// GetMe godoc
// @Summary Get current user
// @Description Get current user profile
// @Tags Auth
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=UserResponse}
// @Failure 401 {object} response.Envelope{error=response.ErrorBlock}
// @Router /auth/me [get]
func (h *Handler) GetMe(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(string)

	res, err := h.service.GetMe(c.Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Profile retrieved successfully", res)
}

// Login godoc
// @Summary Login user
// @Description Login user and return tokens
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login Request"
// @Success 200 {object} response.Envelope{data=TokenResponse}
// @Failure 400 {object} response.Envelope{error=response.ErrorBlock}
// @Failure 401 {object} response.Envelope{error=response.ErrorBlock}
// @Router /auth/login [post]
func (h *Handler) Login(c *fiber.Ctx) error {
	var req LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}

	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	res, err := h.service.Login(c.Context(), req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Login successful", res)
}

// Refresh godoc
// @Summary Refresh token
// @Description Get a new access token using a refresh token
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RefreshRequest false "Refresh Request (if not in cookie)"
// @Success 200 {object} response.Envelope{data=TokenResponse}
// @Failure 401 {object} response.Envelope{error=response.ErrorBlock}
// @Router /auth/refresh [post]
func (h *Handler) Refresh(c *fiber.Ctx) error {
	var req RefreshRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}

	res, err := h.service.Refresh(c.Context(), req.RefreshToken)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusOK, "Token refreshed successfully", res)
}

// Logout godoc
// @Summary Logout user
// @Description Logout user by clearing refresh token cookie
// @Tags Auth
// @Produce json
// @Success 200 {object} response.Envelope
// @Router /auth/logout [post]
func (h *Handler) Logout(c *fiber.Ctx) error {
	// With tokens stored in the body/local storage, logout is primarily a client-side action.
	// If a token blocklist is added, it would be checked/written here.
	return response.Success(c, fiber.StatusOK, "Logged out successfully", nil)
}
