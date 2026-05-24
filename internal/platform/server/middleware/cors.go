package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
)

func CORS() fiber.Handler {
	// Restrict to storefront domain
	return cors.New(cors.Config{
		AllowOrigins:     "http://localhost:3000, https://momiji-home.com, https://momiji-home.vercel.app",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Correlation-ID",
		AllowMethods:     "GET, POST, PUT, PATCH, DELETE, OPTIONS",
		AllowCredentials: true,
	})
}
