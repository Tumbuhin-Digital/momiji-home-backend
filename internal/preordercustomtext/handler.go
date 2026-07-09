package preordercustomtext

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/validator"
)

type Handler struct {
	service   Service
	jwtSecret string
}

func NewHandler(service Service, jwtSecret string) *Handler {
	return &Handler{service: service, jwtSecret: jwtSecret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	group := router.Group("/preorder-custom-texts")
	group.Use(middleware.Auth(h.jwtSecret))
	group.Use(middleware.RBAC("admin"))

	group.Get("/", h.List)
	group.Post("/", h.Create)
	group.Delete("/:id", h.Delete)
}

func (h *Handler) List(c *fiber.Ctx) error {
	var query ListQuery
	if err := c.QueryParser(&query); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}

	items, err := h.service.List(c.Context(), query.Search)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Preorder custom texts retrieved", items)
}

func (h *Handler) Create(c *fiber.Ctx) error {
	var req CreatePreorderCustomTextRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	item, err := h.service.Create(c.Context(), req.Label)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusCreated, "Preorder custom text created", item)
}

func (h *Handler) Delete(c *fiber.Ctx) error {
	result, err := h.service.Delete(c.Context(), c.Params("id"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Preorder custom text deleted", result)
}
