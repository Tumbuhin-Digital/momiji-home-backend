package checkout

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	cartService cart.CartService
	jwtSecret   string
}

func NewCheckoutHandler(cartService cart.CartService, jwtSecret string) *Handler {
	return &Handler{cartService: cartService, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	// Shipping routes (authenticated)
	shipping := router.Group("/shipping")
	shipping.Use(middleware.Auth(h.jwtSecret))
	shipping.Get("/methods", h.GetShippingMethods)
	shipping.Post("/calculate", h.CalculateShipping)

	// Checkout routes (authenticated)
	checkout := router.Group("/checkout")
	checkout.Use(middleware.Auth(h.jwtSecret))
	checkout.Post("/summary", h.GetCheckoutSummary)
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

	uid := c.Locals("user_id").(string)
	
	cartRes, err := h.cartService.GetCartResponse(c.Context(), &uid, nil)
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
