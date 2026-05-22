package server

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/server/middleware"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
	"github.com/gofiber/swagger"
	_ "github.com/tumbuhindigi-sys/momiji-home-backend/docs"
)

func NewFiberApp(log *slog.Logger) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			if e, ok := err.(*fiber.Error); ok {
				return response.Error(c, apierror.New(e.Code, "HTTP_ERROR", e.Message))
			}
			return response.Error(c, err)
		},
	})

	// Global Middleware
	app.Use(recover.New())
	app.Use(middleware.CorrelationID())
	app.Use(middleware.Logger(log))
	app.Use(middleware.CORS())

	// Health checks
	app.Get("/health", func(c *fiber.Ctx) error {
		return response.Success(c, 200, "OK", fiber.Map{"status": "ok"})
	})

	// Swagger Docs
	app.Get("/swagger/*", swagger.HandlerDefault)

	return app
}
