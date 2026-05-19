package order

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	service   OrderService
	jwtSecret string
}

func NewOrderHandler(service OrderService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/orders")

	// Create Order (Guest or Auth)
	createGrp := group.Group("/")
	createGrp.Use(middleware.OptionalAuth(h.jwtSecret))
	createGrp.Post("/", h.CreateOrder)

	// List Orders (Auth Only)
	authGrp := group.Group("/")
	authGrp.Use(middleware.Auth(h.jwtSecret))
	authGrp.Get("/", h.GetOrders)
}

// CreateOrder godoc
// @Summary Create an order from cart
// @Tags Order
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body CreateOrderRequest true "Create Order Request"
// @Success 201 {object} response.Envelope{data=OrderResponse}
// @Router /orders [post]
func (h *Handler) CreateOrder(c *fiber.Ctx) error {
	var req CreateOrderRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	var userID, sessionID *string
	if uid, ok := c.Locals("user_id").(string); ok && uid != "" {
		userID = &uid
	}
	if sid, ok := c.Locals("session_id").(string); ok && sid != "" {
		sessionID = &sid
	}

	res, err := h.service.CreateOrder(c.Context(), userID, sessionID, req)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Success(c, fiber.StatusCreated, "Order created successfully", res)
}

// GetOrders godoc
// @Summary List customer orders
// @Tags Order
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=[]OrderResponse}
// @Router /orders [get]
func (h *Handler) GetOrders(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	res, err := h.service.GetOrders(c.Context(), uid)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Orders retrieved", res)
}
