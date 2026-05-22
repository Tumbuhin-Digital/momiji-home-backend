package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

func RBAC(allowedRoles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		role, ok := c.Locals("role").(string)
		if !ok {
			return response.Error(c, apierror.ErrUnauthorized)
		}

		for _, r := range allowedRoles {
			if r == role {
				return c.Next()
			}
		}

		return response.Error(c, apierror.ErrForbidden)
	}
}
