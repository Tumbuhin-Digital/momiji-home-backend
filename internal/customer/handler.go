package customer

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

type Handler struct {
	service   CustomerService
	jwtSecret string
}

func NewCustomerHandler(service CustomerService, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/customers")
	group.Use(middleware.Auth(h.jwtSecret))
	group.Use(middleware.RBAC("admin"))

	group.Get("/", h.ListCustomers)
	group.Get("/:id", h.GetCustomer)
	group.Get("/:id/orders", h.GetCustomerOrders)
}

// ListCustomers godoc
// @Summary List customers
// @Tags Customer
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number"
// @Param limit query int false "Limit per page"
// @Param search query string false "Search term"
// @Success 200 {object} response.Envelope{data=[]CustomerResponse}
// @Router /customers [get]
func (h *Handler) ListCustomers(c *fiber.Ctx) error {
	var query ListCustomersQuery
	if err := c.QueryParser(&query); err != nil {
		return response.Error(c, err)
	}

	customers, total, err := h.service.ListCustomers(c.Context(), query)
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
		ItemsKey:   "customers",
		Items:      customers,
	}

	return response.Success(c, fiber.StatusOK, "Customers retrieved", paginatedData)
}

// GetCustomer godoc
// @Summary Get customer details
// @Tags Customer
// @Produce json
// @Security BearerAuth
// @Param id path string true "Customer ID"
// @Success 200 {object} response.Envelope{data=CustomerDetailResponse}
// @Router /customers/{id} [get]
func (h *Handler) GetCustomer(c *fiber.Ctx) error {
	customer, err := h.service.GetCustomer(c.Context(), c.Params("id"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Customer retrieved", customer)
}

// GetCustomerOrders godoc
// @Summary Get customer orders
// @Tags Customer
// @Produce json
// @Security BearerAuth
// @Param id path string true "Customer ID"
// @Success 200 {object} response.Envelope{data=[]CustomerOrderResponse}
// @Router /customers/{id}/orders [get]
func (h *Handler) GetCustomerOrders(c *fiber.Ctx) error {
	orders, err := h.service.GetCustomerOrders(c.Context(), c.Params("id"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Customer orders retrieved", orders)
}
