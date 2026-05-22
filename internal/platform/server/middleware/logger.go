package middleware

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

func Logger(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		
		err := c.Next()
		
		duration := time.Since(start)
		correlationID := c.Locals("correlation_id")

		log.Info("HTTP Request",
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
			slog.Int("status", c.Response().StatusCode()),
			slog.String("duration", duration.String()),
			slog.Any("correlation_id", correlationID),
			slog.String("ip", c.IP()),
		)

		return err
	}
}
