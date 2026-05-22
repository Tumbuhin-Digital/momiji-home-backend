package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

// OptionalAuth allows access via Bearer token (authenticated user) OR X-Session-ID header (guest user).
func OptionalAuth(jwtSecret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		sessionID := c.Get("X-Session-ID")

		// Case 1: Has Bearer Token
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				tokenString := parts[1]

				token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, apierror.ErrUnauthorized
					}
					return []byte(jwtSecret), nil
				})

				if err == nil && token.Valid {
					if claims, ok := token.Claims.(jwt.MapClaims); ok {
						c.Locals("user_id", claims["sub"])
						c.Locals("role", claims["role"])
						return c.Next()
					}
				}
				// If token is present but invalid, reject.
				return response.Error(c, apierror.ErrUnauthorized)
			}
		}

		// Case 2: Has Session ID
		if sessionID != "" {
			c.Locals("session_id", sessionID)
			return c.Next()
		}

		// Neither auth mechanisms are present
		return response.Error(c, apierror.ErrUnauthorized)
	}
}
