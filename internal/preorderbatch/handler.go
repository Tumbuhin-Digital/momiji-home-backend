package preorderbatch

import (
	"net/url"

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
	group := router.Group("/")
	group.Use(middleware.Auth(h.jwtSecret))
	group.Use(middleware.RBAC("admin"))

	group.Get("/variants/:variantId/batches", h.ListVariantBatches)
	group.Post("/variants/:variantId/batches", h.CreateBatch)
	group.Patch("/batches/:batchId", h.UpdateBatch)
	group.Post("/batches/:batchId/close", h.CloseBatch)
	group.Post("/batches/:batchId/cancel", h.CancelBatch)
	group.Post("/batches/:batchId/reorder", h.ReorderBatch)
}

func (h *Handler) ListVariantBatches(c *fiber.Ctx) error {
	variantID, err := url.PathUnescape(c.Params("variantId"))
	if err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}
	result, err := h.service.ListVariantBatches(c.Context(), variantID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Variant batches retrieved", result)
}

func (h *Handler) CreateBatch(c *fiber.Ctx) error {
	var req CreateBatchRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	variantID, err := url.PathUnescape(c.Params("variantId"))
	if err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}
	result, err := h.service.CreateBatch(c.Context(), variantID, req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusCreated, "Batch created", result)
}

func (h *Handler) UpdateBatch(c *fiber.Ctx) error {
	var req UpdateBatchRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}

	result, err := h.service.UpdateBatch(c.Context(), c.Params("batchId"), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Batch updated", result)
}

func (h *Handler) CloseBatch(c *fiber.Ctx) error {
	result, err := h.service.CloseBatch(c.Context(), c.Params("batchId"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Batch closed", result)
}

func (h *Handler) CancelBatch(c *fiber.Ctx) error {
	result, err := h.service.CancelBatch(c.Context(), c.Params("batchId"))
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Batch cancelled", result)
}

func (h *Handler) ReorderBatch(c *fiber.Ctx) error {
	var req ReorderBatchRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apierror.ErrBadRequest)
	}
	if err := validator.ValidateStruct(&req); err != nil {
		return response.Error(c, err)
	}
	result, err := h.service.ReorderBatch(c.Context(), c.Params("batchId"), req.Sequence)
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, fiber.StatusOK, "Batch reordered", result)
}
