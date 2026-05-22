package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func CorrelationID() fiber.Handler {
	return func(c *fiber.Ctx) error {
		reqID := c.Get("X-Correlation-ID")
		if reqID == "" {
			reqID = uuid.New().String()
		}
		
		c.Locals("correlation_id", reqID)
		c.Set("X-Correlation-ID", reqID)
		
		return c.Next()
	}
}
