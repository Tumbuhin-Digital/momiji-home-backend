package response

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type Envelope struct {
	Status        string      `json:"status"` // "success" or "error"
	Data          interface{} `json:"data,omitempty"`
	Meta          interface{} `json:"meta,omitempty"`
	Error         *ErrorBlock `json:"error,omitempty"`
}

type ErrorBlock struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	CorrelationID string `json:"correlationId,omitempty"`
}

func Success(c *fiber.Ctx, status int, data interface{}, meta interface{}) error {
	return c.Status(status).JSON(Envelope{
		Status: "success",
		Data:   data,
		Meta:   meta,
	})
}

func Error(c *fiber.Ctx, err error) error {
	appErr, ok := err.(*apierror.AppError)
	if !ok {
		appErr = apierror.ErrInternal
	}

	correlationID, _ := c.Locals("correlation_id").(string)

	return c.Status(appErr.Status).JSON(Envelope{
		Status: "error",
		Error: &ErrorBlock{
			Code:          appErr.Code,
			Message:       appErr.Message,
			CorrelationID: correlationID,
		},
	})
}
