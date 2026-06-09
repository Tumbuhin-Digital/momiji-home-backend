package checkout

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	cartService     cart.CartService
	checkoutService CheckoutService
	orderService    order.OrderService
	jwtSecret       string
}

func NewCheckoutHandler(cartService cart.CartService, checkoutService CheckoutService, orderService order.OrderService, jwtSecret string) *Handler {
	return &Handler{cartService: cartService, checkoutService: checkoutService, orderService: orderService, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	// Shipping routes (authenticated)
	shipping := router.Group("/shipping")
	shipping.Use(middleware.OptionalAuth(h.jwtSecret))
	shipping.Get("/methods", h.GetShippingMethods)
	shipping.Post("/calculate", h.CalculateShipping)

	// Checkout routes (authenticated)
	checkout := router.Group("/checkout")
	checkout.Use(middleware.OptionalAuth(h.jwtSecret))
	checkout.Post("/summary", h.GetCheckoutSummary)
	checkout.Post("/", h.InitiateCheckout)
	checkout.Get("/confirm", h.GetCheckoutConfirm)
}

func (h *Handler) extractIdentity(c *fiber.Ctx) (userID *string, sessionID *string) {
	if uid, ok := c.Locals("user_id").(string); ok && uid != "" {
		userID = &uid
	}
	if sid, ok := c.Locals("session_id").(string); ok && sid != "" {
		sessionID = &sid
	}
	return
}

// GetShippingMethods godoc
// @Summary List shipping methods
// @Tags Shipping
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=ShippingMethodsResponse}
// @Router /shipping/methods [get]
func (h *Handler) GetShippingMethods(c *fiber.Ctx) error {
	// Mock response
	res := &ShippingMethodsResponse{
		Methods: []ShippingMethod{
			{ID: "ground", Label: "Ground", EstimatedArrival: "5-7 Business Days", Cost: "20.00"},
			{ID: "expedited", Label: "Expedited", EstimatedArrival: "2-3 Business Days", Cost: "35.00"},
		},
		Currency: "USD",
	}
	return response.Success(c, fiber.StatusOK, "Shipping methods retrieved", res)
}

// CalculateShipping godoc
// @Summary Calculate shipping cost
// @Tags Shipping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security SessionAuth
// @Param request body CalculateShippingRequest true "Calculate Request"
// @Success 200 {object} response.Envelope
// @Router /shipping/calculate [post]
func (h *Handler) CalculateShipping(c *fiber.Ctx) error {
	var req CalculateShippingRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}
	
	// Mock implementation
	cost := "20.00"
	if req.ShippingMethod == "expedited" {
		cost = "35.00"
	}
	
	return response.Success(c, fiber.StatusOK, "Shipping calculated", fiber.Map{"cost": cost, "currency": "USD"})
}

// GetCheckoutSummary godoc
// @Summary Get checkout summary
// @Tags Checkout
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security SessionAuth
// @Param request body CheckoutSummaryRequest true "Checkout Summary Request"
// @Success 200 {object} response.Envelope{data=CheckoutSummaryResponse}
// @Router /checkout/summary [post]
func (h *Handler) GetCheckoutSummary(c *fiber.Ctx) error {
	var req CheckoutSummaryRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	uid, sid := h.extractIdentity(c)
	
	cartRes, err := h.cartService.GetCartResponse(c.Context(), uid, sid)
	if err != nil {
		return response.Error(c, err)
	}

	shippingCost := 20.00
	if req.ShippingMethod == "expedited" {
		shippingCost = 35.00
	}

	var totalShipReady, preorderDeposit, preorderBalance float64
	fmt.Sscanf(cartRes.Summary.TotalShipReady, "%f", &totalShipReady)
	fmt.Sscanf(cartRes.Summary.TotalDeposit, "%f", &preorderDeposit)
	fmt.Sscanf(cartRes.Summary.TotalBalanceDue, "%f", &preorderBalance)

	res := &CheckoutSummaryResponse{}
	res.ShipReady.Items = cartRes.ShipReady
	res.ShipReady.Subtotal = cartRes.Summary.TotalShipReady
	
	res.PreOrder.Items = cartRes.PreOrder
	res.PreOrder.DepositSubtotal = cartRes.Summary.TotalDeposit
	res.PreOrder.BalanceSubtotal = cartRes.Summary.TotalBalanceDue
	
	res.Shipping.Method = req.ShippingMethod
	res.Shipping.Cost = fmt.Sprintf("%.2f", shippingCost)
	res.Shipping.EstimatedArrival = "5-7 Business Days"

	dueNowTotal := totalShipReady + shippingCost + preorderDeposit
	res.DueNow.ShipReadyTotal = cartRes.Summary.TotalShipReady
	res.DueNow.Shipping = res.Shipping.Cost
	res.DueNow.PreorderDeposit = cartRes.Summary.TotalDeposit
	res.DueNow.Total = fmt.Sprintf("%.2f", dueNowTotal)
	
	// Preorder balance assumes a flat shipping rate or free later
	shippingPreorder := 10.00
	if len(cartRes.PreOrder) == 0 {
		shippingPreorder = 0
	}
	
	dueAugustTotal := preorderBalance + shippingPreorder
	res.DueAugust.PreorderBalance = cartRes.Summary.TotalBalanceDue
	res.DueAugust.ShippingPreorder = fmt.Sprintf("%.2f", shippingPreorder)
	res.DueAugust.Total = fmt.Sprintf("%.2f", dueAugustTotal)

	res.Currency = "USD"

	return response.Success(c, fiber.StatusOK, "Checkout summary retrieved", res)
}

// InitiateCheckout godoc
// @Summary Initiate Shopify checkout
// @Tags Checkout
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security SessionAuth
// @Param request body InitiateCheckoutRequest true "Initiate Checkout Request"
// @Success 200 {object} response.Envelope{data=InitiateCheckoutResponse}
// @Router /checkout [post]
func (h *Handler) InitiateCheckout(c *fiber.Ctx) error {
	var req InitiateCheckoutRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	uid, sid := h.extractIdentity(c)
	
	// Guest must provide email if not logged in
	if uid == nil && req.Email == "" {
		return response.Error(c, apierror.New(400, "validation_error", "email is required for guest checkout"))
	}

	res, err := h.checkoutService.InitiateCheckout(c.Context(), uid, sid, req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Checkout initiated", res)
}

// GetCheckoutConfirm godoc
// @Summary Get checkout confirmation status
// @Tags Checkout
// @Produce json
// @Security BearerAuth
// @Param shopify_order_id query string true "Shopify Order ID"
// @Success 200 {object} response.Envelope{data=map[string]interface{}}
// @Failure 404 {object} response.Envelope
// @Router /checkout/confirm [get]
func (h *Handler) GetCheckoutConfirm(c *fiber.Ctx) error {
	orderID := c.Query("shopify_order_id")
	if orderID == "" {
		return response.Error(c, apierror.New(400, "bad_request", "shopify_order_id is required"))
	}
	
	// Fetch from OrderService
	orderRes, err := h.orderService.GetOrderByShopifyID(c.Context(), orderID)
	if err != nil {
		if err == apierror.ErrNotFound {
			return response.Error(c, apierror.New(404, "not_found", "Order not yet confirmed by webhook"))
		}
		return response.Error(c, err)
	}
	
	return response.Success(c, fiber.StatusOK, "Order confirmed", fiber.Map{
		"order_id":         orderRes.ID,
		"order_number":     orderRes.OrderNumber,
		"status":           orderRes.FinancialStatus,
		"shopify_order_id": orderID,
	})
}
