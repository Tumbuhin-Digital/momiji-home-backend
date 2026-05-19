package cart

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	service   CartService
	jwtSecret string
}

func NewCartHandler(service CartService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/cart")

	group.Post("/session", h.CreateSession)

	// endpoints that accept either Bearer or X-Session-ID
	optAuth := group.Group("/")
	optAuth.Use(middleware.OptionalAuth(h.jwtSecret))
	
	optAuth.Get("/", h.GetCart)
	optAuth.Get("/summary", h.GetSummary)
	optAuth.Post("/items", h.AddItem)
	optAuth.Patch("/items/:id", h.UpdateItem)
	optAuth.Delete("/items/:id", h.RemoveItem)
	optAuth.Delete("/", h.ClearCart)

	// authenticated only endpoint for merge
	auth := group.Group("/")
	auth.Use(middleware.Auth(h.jwtSecret))
	auth.Post("/merge", h.MergeCart)
}

// CreateSession godoc
// @Summary Create Guest Cart Session
// @Tags Cart
// @Produce json
// @Success 201 {object} response.Envelope{data=GuestSessionResponse}
// @Router /cart/session [post]
func (h *Handler) CreateSession(c *fiber.Ctx) error {
	res, err := h.service.CreateGuestSession(c.Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusCreated, "Session created", res)
}

func (h *Handler) extractAuth(c *fiber.Ctx) (*string, *string) {
	var userID, sessionID *string
	if uid, ok := c.Locals("user_id").(string); ok && uid != "" {
		userID = &uid
	}
	if sid, ok := c.Locals("session_id").(string); ok && sid != "" {
		sessionID = &sid
	}
	return userID, sessionID
}

// GetCart godoc
// @Summary Get Cart
// @Tags Cart
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=CartResponse}
// @Router /cart [get]
func (h *Handler) GetCart(c *fiber.Ctx) error {
	uid, sid := h.extractAuth(c)
	res, err := h.service.GetCartResponse(c.Context(), uid, sid)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Cart retrieved", res)
}

// GetSummary godoc
// @Summary Get Cart Summary
// @Tags Cart
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=CartSummaryDTO}
// @Router /cart/summary [get]
func (h *Handler) GetSummary(c *fiber.Ctx) error {
	uid, sid := h.extractAuth(c)
	res, err := h.service.GetCartSummary(c.Context(), uid, sid)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Cart summary retrieved", res)
}

// AddItem godoc
// @Summary Add item to cart
// @Tags Cart
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CartItemRequest true "Add Item Request"
// @Success 200 {object} response.Envelope
// @Router /cart/items [post]
func (h *Handler) AddItem(c *fiber.Ctx) error {
	uid, sid := h.extractAuth(c)
	var req CartItemRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	if err := h.service.AddItem(c.Context(), uid, sid, req); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Item added to cart", nil)
}

// UpdateItem godoc
// @Summary Update item quantity
// @Tags Cart
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Item ID"
// @Param request body UpdateCartItemRequest true "Update Quantity Request"
// @Success 200 {object} response.Envelope
// @Router /cart/items/{id} [patch]
func (h *Handler) UpdateItem(c *fiber.Ctx) error {
	uid, sid := h.extractAuth(c)
	itemID := c.Params("id")
	
	var req UpdateCartItemRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	if err := h.service.UpdateItemQuantity(c.Context(), uid, sid, itemID, req); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Item updated", nil)
}

// RemoveItem godoc
// @Summary Remove item from cart
// @Tags Cart
// @Produce json
// @Security BearerAuth
// @Param id path string true "Item ID"
// @Success 200 {object} response.Envelope
// @Router /cart/items/{id} [delete]
func (h *Handler) RemoveItem(c *fiber.Ctx) error {
	uid, sid := h.extractAuth(c)
	itemID := c.Params("id")
	
	if err := h.service.RemoveItem(c.Context(), uid, sid, itemID); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Item removed", nil)
}

// ClearCart godoc
// @Summary Clear all cart items
// @Tags Cart
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope
// @Router /cart [delete]
func (h *Handler) ClearCart(c *fiber.Ctx) error {
	uid, sid := h.extractAuth(c)
	if err := h.service.ClearCart(c.Context(), uid, sid); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Cart cleared", nil)
}

// MergeCart godoc
// @Summary Merge guest cart into user cart
// @Tags Cart
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body MergeCartRequest true "Merge Request"
// @Success 200 {object} response.Envelope
// @Router /cart/merge [post]
func (h *Handler) MergeCart(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	var req MergeCartRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	if err := h.service.MergeCarts(c.Context(), uid, req.GuestSessionID); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Cart merged", nil)
}
