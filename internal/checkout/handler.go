package checkout

import (
	"fmt"
	"strconv"

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
	shipping.Post("/rates", h.GetShippingRates)
	shipping.Post("/validate-address", h.ValidateAddress)

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

// GetShippingRates godoc
// @Summary List live shipping rates via ShipStation
// @Tags Shipping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body ShippingRatesRequest true "Shipping Rates Request"
// @Success 200 {object} response.Envelope{data=[]ShippingRateDTO}
// @Router /shipping/rates [post]
func (h *Handler) GetShippingRates(c *fiber.Ctx) error {
	uid, sid := h.extractIdentity(c)

	var req ShippingRatesRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apierror.New(400, "invalid_request", "invalid request body"))
	}

	if req.Zip == "" {
		return response.Error(c, apierror.New(400, "invalid_request", "zip code is required"))
	}
	if req.Country == "" {
		req.Country = "US"
	}

	rates, err := h.checkoutService.GetShippingRates(c.Context(), uid, sid, req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Shipping rates retrieved", rates)
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
	estimatedArrival := "5-7 Business Days"

	// Fetch dynamic shipping rates from ShipStation if zip code is provided
	if req.Zip != "" {
		ratesReq := ShippingRatesRequest{
			Zip:     req.Zip,
			Country: req.Country,
		}
		if ratesReq.Country == "" {
			ratesReq.Country = "US"
		}
		rates, rateErr := h.checkoutService.GetShippingRates(c.Context(), uid, sid, ratesReq)
		if rateErr == nil && len(rates) > 0 {
			// Find matching service code, or fallback to the first rate if method not found
			var matchedRate *ShippingRateDTO
			for _, r := range rates {
				if r.ServiceCode == req.ShippingMethod {
					matchedRate = &r
					break
				}
			}
			if matchedRate == nil && req.ShippingMethod == "" {
				matchedRate = &rates[0] // default to first available
			}
			if matchedRate != nil {
				if cost, err := strconv.ParseFloat(matchedRate.Cost, 64); err == nil {
					shippingCost = cost
				}
				if matchedRate.DeliveryDays != nil {
					estimatedArrival = fmt.Sprintf("%d Days", *matchedRate.DeliveryDays)
				}
			}
		}
	} else if req.ShippingMethod == "expedited" {
		shippingCost = 35.00
		estimatedArrival = "2-3 Business Days"
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
	res.Shipping.EstimatedArrival = estimatedArrival

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
// @Param checkout_reference query string true "Checkout Reference UUID"
// @Success 200 {object} response.Envelope{data=map[string]interface{}}
// @Failure 404 {object} response.Envelope
// @Router /checkout/confirm [get]
func (h *Handler) GetCheckoutConfirm(c *fiber.Ctx) error {
	orderID := c.Query("checkout_reference")
	if orderID == "" {
		// Fallback for older frontend polling implementations
		orderID = c.Query("shopify_order_id")
	}
	if orderID == "" {
		return response.Error(c, apierror.New(400, "bad_request", "checkout_reference is required"))
	}

	// Fetch from OrderService
	orderRes, err := h.orderService.GetOrderByShopifyID(c.Context(), orderID)
	if err != nil {
		if err == apierror.ErrNotFound {
			return response.Error(c, apierror.New(404, "not_found", "Order not yet confirmed by webhook"))
		}
		return response.Error(c, err)
	}
	// Build items list (all line items, ship_ready + pre_order combined)
	var items []fiber.Map
	for _, it := range orderRes.LineItems.ShipReady {
		items = append(items, fiber.Map{
			"title":          it.Title,
			"quantity":       it.Quantity,
			"type":           "ship_ready",
			"item_status":    it.ItemStatus,
			"amount_charged": it.AmountCharged,
			"balance_due":    it.BalanceDue,
		})
	}
	for _, it := range orderRes.LineItems.PreOrder {
		items = append(items, fiber.Map{
			"title":          it.Title,
			"quantity":       it.Quantity,
			"type":           "pre_order",
			"item_status":    it.ItemStatus,
			"amount_charged": it.AmountCharged,
			"balance_due":    it.BalanceDue,
		})
	}
	if items == nil {
		items = []fiber.Map{}
	}

	customerEmail := ""
	if orderRes.Customer != nil {
		customerEmail = orderRes.Customer.Email
	}

	return response.Success(c, fiber.StatusOK, "Order confirmed", fiber.Map{
		"order_id":          orderRes.ID,
		"order_number":      orderRes.OrderNumber,
		"order_date":        orderRes.OrderDate,
		"customer_email":    customerEmail,
		"total_price":       orderRes.TotalPrice,
		"total_charged_now": orderRes.TotalChargedNow,
		"total_balance_due": orderRes.TotalBalanceDue,
		"currency":          orderRes.Currency,
		"financial_status":  orderRes.FinancialStatus,
		"items":             items,
	})
}

// ValidateAddress godoc
// @Summary Validate shipping address
// @Tags Shipping
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security SessionAuth
// @Param request body ValidateAddressRequest true "Validate Address Request"
// @Success 200 {object} response.Envelope
// @Failure 422 {object} response.Envelope
// @Router /shipping/validate-address [post]
func (h *Handler) ValidateAddress(c *fiber.Ctx) error {
	var req ValidateAddressRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	errs := h.checkoutService.ValidateAddress(c.Context(), req)
	if len(errs) > 0 {
		return c.Status(fiber.StatusUnprocessableEntity).JSON(response.Envelope{
			Status:  "error",
			Message: "Address validation failed",
			Error: &response.ErrorBlock{
				Code:    "validation_error",
				Details: errs,
			},
		})
	}

	return response.Success(c, fiber.StatusOK, "Address is valid", nil)
}
