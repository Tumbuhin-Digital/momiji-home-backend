package warehouse

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

type Handler struct {
	service   Service
	jwtSecret string
}

func NewHandler(service Service, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/settings/warehouses")
	group.Use(middleware.Auth(h.jwtSecret))
	group.Use(middleware.RBAC("admin"))
	group.Get("/", h.ListWarehouses)
	group.Patch("/:code", h.UpdateWarehouse)
}

// ListWarehouses godoc
// @Summary List configured warehouses
// @Tags Settings
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=WarehouseListResponse}
// @Router /settings/warehouses [get]
func (h *Handler) ListWarehouses(c *fiber.Ctx) error {
	res, err := h.service.List(c.Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Warehouses retrieved", res)
}

// UpdateWarehouse godoc
// @Summary Update warehouse address by code
// @Tags Settings
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param code path string true "Warehouse code (east|west)"
// @Param body body UpdateWarehouseRequest true "Warehouse payload"
// @Success 200 {object} response.Envelope{data=WarehouseDTO}
// @Router /settings/warehouses/{code} [patch]
func (h *Handler) UpdateWarehouse(c *fiber.Ctx) error {
	code := c.Params("code")
	if code != CodeEast && code != CodeWest {
		return response.Error(c, apierror.New(400, "invalid_request", "warehouse code must be east or west"))
	}

	var req UpdateWarehouseRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}

	updated, err := h.service.Update(c.Context(), code, req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Warehouse updated successfully", updated)
}
