package response

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type Envelope struct {
	Status    string      `json:"status"` // "success" or "error"
	Message   string      `json:"message,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     *ErrorBlock `json:"error,omitempty"`
	Timestamp string      `json:"timestamp"`
}

type ErrorBlock struct {
	Code    string      `json:"code"`
	Details interface{} `json:"details,omitempty"`
}

type Meta struct {
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
}

func Success(c *fiber.Ctx, status int, message string, data interface{}) error {
	return c.Status(status).JSON(Envelope{
		Status:    "success",
		Message:   message,
		Data:      data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func SuccessWithMeta(c *fiber.Ctx, status int, message string, data interface{}, meta Meta) error {
	type envelopeWithMeta struct {
		Status    string      `json:"status"`
		Message   string      `json:"message,omitempty"`
		Data      interface{} `json:"data,omitempty"`
		Meta      Meta        `json:"meta"`
		Timestamp string      `json:"timestamp"`
	}
	return c.Status(status).JSON(envelopeWithMeta{
		Status:    "success",
		Message:   message,
		Data:      data,
		Meta:      meta,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func Error(c *fiber.Ctx, err error) error {
	appErr, ok := err.(*apierror.AppError)
	if !ok {
		appErr = apierror.ErrInternal
	}

	return c.Status(appErr.Status).JSON(Envelope{
		Status:  "error",
		Message: appErr.Message,
		Error: &ErrorBlock{
			Code:    appErr.Code,
			Details: appErr.Details,
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}
