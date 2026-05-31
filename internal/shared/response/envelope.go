package response

import (
	"encoding/json"
	"log/slog"
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
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int   `json:"totalPages"`
}

// PaginatedData helper construct to dynamically marshal a data collection under a specific key.
type PaginatedData struct {
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	Total      int64       `json:"total"`
	TotalPages int         `json:"totalPages"`
	ItemsKey   string      `json:"-"`
	Items      interface{} `json:"-"`
}

func (p PaginatedData) MarshalJSON() ([]byte, error) {
	m := map[string]interface{}{
		"page":       p.Page,
		"limit":      p.Limit,
		"total":      p.Total,
		"totalPages": p.TotalPages,
	}
	m[p.ItemsKey] = p.Items
	return json.Marshal(m)
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
		// Log the unhandled internal error here with details
		slog.Error("Internal server error", 
			slog.Any("error", err),
			slog.String("path", c.Path()),
			slog.String("method", c.Method()),
		)
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
