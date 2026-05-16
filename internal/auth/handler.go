package auth

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	service AuthService
}

func NewAuthHandler(service AuthService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/auth")
	
	rateLimit := middleware.RateLimit(5, 15*time.Minute)

	group.Post("/register", rateLimit, h.Register)
	group.Post("/login", rateLimit, h.Login)
	group.Post("/refresh", h.Refresh)
	group.Post("/logout", h.Logout)
}

// Register godoc
// @Summary Register a new user
// @Description Register a new user and return tokens
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body RegisterRequest true "Register Request"
// @Success 201 {object} response.Envelope{data=TokenResponse}
// @Failure 400 {object} response.Envelope{error=response.ErrorBlock}
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

	h.setRefreshCookie(c, res.RefreshToken)

	return response.Success(c, fiber.StatusCreated, res, nil)
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

	h.setRefreshCookie(c, res.RefreshToken)

	return response.Success(c, fiber.StatusOK, res, nil)
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
	refreshToken := c.Cookies("refresh_token")
	if refreshToken == "" {
		// Fallback to body
		var req RefreshRequest
		if err := c.BodyParser(&req); err == nil {
			refreshToken = req.RefreshToken
		}
	}

	if refreshToken == "" {
		return response.Error(c, fiber.ErrUnauthorized)
	}

	res, err := h.service.Refresh(c.Context(), refreshToken)
	if err != nil {
		return response.Error(c, err)
	}

	h.setRefreshCookie(c, res.RefreshToken)

	return response.Success(c, fiber.StatusOK, res, nil)
}

// Logout godoc
// @Summary Logout user
// @Description Logout user by clearing refresh token cookie
// @Tags Auth
// @Produce json
// @Success 200 {object} response.Envelope
// @Router /auth/logout [post]
func (h *Handler) Logout(c *fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour),
		HTTPOnly: true,
		Secure:   true,
		SameSite: "Strict",
	})

	return response.Success(c, fiber.StatusOK, fiber.Map{"message": "Logged out successfully"}, nil)
}

func (h *Handler) setRefreshCookie(c *fiber.Ctx, token string) {
	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    token,
		Expires:  time.Now().Add(7 * 24 * time.Hour), // 7 days
		HTTPOnly: true,
		Secure:   true,
		SameSite: "Strict",
	})
}
