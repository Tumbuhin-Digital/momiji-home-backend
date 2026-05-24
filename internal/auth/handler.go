package auth

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	service      AuthService
	jwtSecret    string
	secureCookie bool
}

func NewAuthHandler(service AuthService, jwtSecret string, secureCookie bool) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret, secureCookie: secureCookie}
}

func (h *Handler) getSameSite() string {
	if h.secureCookie {
		return "None" // For production/staging cross-origin
	}
	return "Lax" // For local development (HTTP)
}

func (h *Handler) setTokenCookies(c *fiber.Ctx, accessToken, refreshToken string) {
	sameSite := h.getSameSite()
	c.Cookie(&fiber.Cookie{
		Name:     "access_token",
		Value:    accessToken,
		HTTPOnly: true,
		Secure:   h.secureCookie,
		SameSite: sameSite,
		Path:     "/",
		MaxAge:   900, // 15 minutes
	})
	c.Cookie(&fiber.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		HTTPOnly: true,
		Secure:   h.secureCookie,
		SameSite: sameSite,
		Path:     "/",
		MaxAge:   604800, // 7 days
	})
}

func (h *Handler) clearTokenCookies(c *fiber.Ctx) {
	for _, name := range []string{"access_token", "refresh_token"} {
		c.Cookie(&fiber.Cookie{
			Name:     name,
			Value:    "",
			HTTPOnly: true,
			Secure:   h.secureCookie,
			SameSite: "Strict",
			MaxAge:   -1,
		})
	}
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
// @Summary Register a new user
// @Description Register a new user and return tokens
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body auth.RegisterRequest true "Register Request"
// @Success 201 {object} response.Envelope{data=auth.TokenResponse}
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

	h.setTokenCookies(c, res.AccessToken, res.RefreshToken)

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

	h.setTokenCookies(c, res.AccessToken, res.RefreshToken)

	return response.Success(c, fiber.StatusOK, "Login successful", res)
}

// Refresh godoc
// @Summary Refresh token
// @Description Get a new access token using a refresh token (no body required)
// @Tags Auth
// @Produce json
// @Success 200 {object} response.Envelope{data=auth.TokenResponse}
// @Failure 401 {object} response.Envelope{error=response.ErrorBlock}
// @Router /auth/refresh [post]
func (h *Handler) Refresh(c *fiber.Ctx) error {
	token := c.Cookies("refresh_token")
	if token == "" {
		return response.Error(c, apierror.ErrUnauthorized)
	}

	res, err := h.service.Refresh(c.Context(), token)
	if err != nil {
		return response.Error(c, err)
	}

	h.setTokenCookies(c, res.AccessToken, res.RefreshToken)

	return response.Success(c, fiber.StatusOK, "Token refreshed successfully", map[string]int{"expires_in": res.ExpiresIn})
}

// Logout godoc
// @Summary Logout user
// @Description Logout user by clearing refresh token cookie
// @Tags Auth
// @Produce json
// @Success 200 {object} response.Envelope
// @Router /auth/logout [post]
func (h *Handler) Logout(c *fiber.Ctx) error {
	h.clearTokenCookies(c)
	return response.Success(c, fiber.StatusOK, "Logged out successfully", nil)
}
