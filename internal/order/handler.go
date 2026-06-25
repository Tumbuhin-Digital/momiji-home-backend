package order

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
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

	// Admin routes (must be defined before /:id)
	adminGrp := group.Group("/", middleware.Auth(h.jwtSecret), middleware.RBAC("admin"))
	adminGrp.Get("/export", h.ExportOrders)
	adminGrp.Post("/:id/preorder/calculate-shipping", h.CalculatePreorderShipping)
	adminGrp.Put("/:id/preorder/shipping", h.UpdatePreorderShipping)
	adminGrp.Post("/:id/preorder/request-second-payment", h.RequestSecondPayment)
	adminGrp.Post("/:id/fulfillments", h.CreateFulfillment)
	adminGrp.Post("/:id/fulfillments/:fulfillmentId/delivered", h.MarkFulfillmentDelivered)

	// List Orders (Auth Only)
	authGrp := group.Group("/")
	authGrp.Use(middleware.Auth(h.jwtSecret))
	authGrp.Get("/", h.GetOrders)
	authGrp.Get("/:id", h.GetOrder)
	authGrp.Patch("/:id/accept", h.AcceptOrder)
	authGrp.Patch("/:id/cancel", h.CancelOrder)
	authGrp.Patch("/:id/items/:itemId/step", h.UpdateFulfillmentStep)
	authGrp.Patch("/:id/received", h.UpdateItemsReceived)
	authGrp.Patch("/:id/tracking", h.AddTrackingNumber)
	authGrp.Get("/:id/tracking", h.GetTracking)
}

// CreateOrder godoc
// @Summary Create an order from cart
// @Tags Order
// @Accept json
// @Produce json
// @Security BearerAuth
// @Security SessionAuth
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
	role := c.Locals("role").(string)

	var query OrderQuery
	if err := c.QueryParser(&query); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}

	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	res, total, err := h.service.GetOrders(c.Context(), customerID, query)
	if err != nil {
		return response.Error(c, err)
	}

	limit := query.Limit
	if limit < 1 { limit = 20 }
	page := query.Page
	if page < 1 { page = 1 }
	totalPages := int((total + int64(limit) - 1) / int64(limit))

	paginatedData := response.PaginatedData{
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages,
		ItemsKey:   "orders",
		Items:      res,
	}

	return response.Success(c, fiber.StatusOK, "Orders retrieved", paginatedData)
}

// GetOrder godoc
// @Summary Get customer order
// @Tags Order
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Success 200 {object} response.Envelope{data=OrderResponse}
// @Router /orders/{id} [get]
func (h *Handler) GetOrder(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	role := c.Locals("role").(string)

	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	res, err := h.service.GetOrder(c.Context(), customerID, c.Params("id"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Order retrieved", res)
}

// AcceptOrder godoc
// @Summary Accept an order
// @Tags Order
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Param request body AcceptOrderRequest true "Accept Order Request"
// @Success 200 {object} response.Envelope
// @Router /orders/{id}/accept [patch]
func (h *Handler) AcceptOrder(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	role := c.Locals("role").(string)
	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	var req AcceptOrderRequest
	if err := c.BodyParser(&req); err != nil { return response.Error(c, err) }
	if err := validator.ValidateStruct(&req); err != nil { return response.Error(c, err) }

	if err := h.service.AcceptOrder(c.Context(), customerID, c.Params("id"), req.FulfillmentType); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Order accepted", nil)
}

// CancelOrder godoc
// @Summary Cancel an order
// @Tags Order
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Param request body CancelOrderRequest true "Cancel Order Request"
// @Success 200 {object} response.Envelope
// @Router /orders/{id}/cancel [patch]
func (h *Handler) CancelOrder(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	role := c.Locals("role").(string)
	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	var req CancelOrderRequest
	if err := c.BodyParser(&req); err != nil { return response.Error(c, err) }
	if err := validator.ValidateStruct(&req); err != nil { return response.Error(c, err) }

	if err := h.service.CancelOrder(c.Context(), customerID, c.Params("id"), req.FulfillmentType, req.Reason); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Order cancelled", nil)
}

// UpdateFulfillmentStep godoc
// @Summary Update fulfillment step
// @Tags Order
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Param itemId path string true "Item ID"
// @Param request body UpdateStepRequest true "Update Step Request"
// @Success 200 {object} response.Envelope
// @Router /orders/{id}/items/{itemId}/step [patch]
func (h *Handler) UpdateFulfillmentStep(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	role := c.Locals("role").(string)
	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	var req UpdateStepRequest
	if err := c.BodyParser(&req); err != nil { return response.Error(c, err) }
	if err := validator.ValidateStruct(&req); err != nil { return response.Error(c, err) }

	if err := h.service.UpdateFulfillmentStep(c.Context(), customerID, c.Params("id"), c.Params("itemId"), req.FulfillmentStep); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Fulfillment step updated", nil)
}

// UpdateItemsReceived godoc
// @Summary Update items received count
// @Tags Order
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Param request body UpdateReceivedRequest true "Update Received Request"
// @Success 200 {object} response.Envelope
// @Router /orders/{id}/received [patch]
func (h *Handler) UpdateItemsReceived(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	role := c.Locals("role").(string)
	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	var req UpdateReceivedRequest
	if err := c.BodyParser(&req); err != nil { return response.Error(c, err) }
	if err := validator.ValidateStruct(&req); err != nil { return response.Error(c, err) }

	if err := h.service.UpdateItemsReceived(c.Context(), customerID, c.Params("id"), req.Items); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Items received updated", nil)
}

// AddTrackingNumber godoc
// @Summary Add tracking number to an item
// @Tags Order
// @Accept json
// @Produce json
// @Security BearerAuth
// @Router /orders/{id}/tracking [patch]
func (h *Handler) AddTrackingNumber(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	role := c.Locals("role").(string)
	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	var req AddTrackingRequest
	if err := c.BodyParser(&req); err != nil { return response.Error(c, err) }
	if err := validator.ValidateStruct(&req); err != nil { return response.Error(c, err) }

	if err := h.service.AddTrackingNumber(c.Context(), customerID, c.Params("id"), req.ItemIDs, req.TrackingNumber, req.TrackingURL); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Tracking number added", nil)
}

// GetTracking godoc
// @Summary Get live tracking status for an order
// @Tags Order
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID"
// @Success 200 {object} response.Envelope{data=[]shipstation.TrackingResponse}
// @Router /orders/{id}/tracking [get]
func (h *Handler) GetTracking(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	orderID := c.Params("id")

	res, err := h.service.GetOrderTracking(c.Context(), uid, orderID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Tracking retrieved", res)
}

// ExportOrders godoc
// @Summary Export orders to Excel (Admin only)
// @Tags Order
// @Produce application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security BearerAuth
// @Param status query string false "Filter by status"
// @Param search query string false "Search by order number or customer email"
// @Success 200 {file} file "sales_report.xlsx"
// @Router /orders/export [get]
func (h *Handler) ExportOrders(c *fiber.Ctx) error {
	var query OrderQuery
	if err := c.QueryParser(&query); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}

	excelBytes, err := h.service.ExportOrdersToExcel(c.Context(), query)
	if err != nil {
		return response.Error(c, err)
	}

	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", `attachment; filename="sales_report.xlsx"`)
	return c.Send(excelBytes)
}

func (h *Handler) CalculatePreorderShipping(c *fiber.Ctx) error {
	var req CalculatePreorderShippingRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	res, err := h.service.CalculatePreorderShipping(c.Context(), "", c.Params("id"), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Shipping calculated", res)
}

func (h *Handler) UpdatePreorderShipping(c *fiber.Ctx) error {
	var req UpdatePreorderShippingRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	res, err := h.service.UpdatePreorderShipping(c.Context(), "", c.Params("id"), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Shipping updated", res)
}

func (h *Handler) RequestSecondPayment(c *fiber.Ctx) error {
	if err := h.service.RequestSecondPayment(c.Context(), "", c.Params("id")); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Second payment invoice sent", nil)
}

func (h *Handler) CreateFulfillment(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	role := c.Locals("role").(string)
	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	var req CreateFulfillmentRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, err)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	res, err := h.service.CreatePreorderFulfillment(c.Context(), customerID, c.Params("id"), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusCreated, "Fulfillment created", res)
}

func (h *Handler) MarkFulfillmentDelivered(c *fiber.Ctx) error {
	uid := c.Locals("user_id").(string)
	role := c.Locals("role").(string)
	customerID := uid
	if role == "admin" {
		customerID = ""
	}

	if err := h.service.MarkFulfillmentDelivered(c.Context(), customerID, c.Params("id"), c.Params("fulfillmentId")); err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Fulfillment marked as delivered", nil)
}
