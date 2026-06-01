package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/response"
)

type Handler struct {
	service       WebhookService
	webhookSecret string
}

func NewWebhookHandler(service WebhookService, secret string) *Handler {
	return &Handler{service: service, webhookSecret: secret}
}

func (h *Handler) SetupRoutes(router fiber.Router) {
	webhooks := router.Group("/webhooks/shopify")
	webhooks.Use(h.verifyShopifyHMAC)
	webhooks.Post("/orders/paid", h.HandleOrderPaid)
	webhooks.Post("/inventory_levels/update", h.HandleInventoryUpdate)
}

func (h *Handler) verifyShopifyHMAC(c *fiber.Ctx) error {
	hmacHeader := c.Get("X-Shopify-Hmac-Sha256")
	if hmacHeader == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing HMAC header"})
	}

	body := c.Body()
	
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)
	expectedHMAC := base64.StdEncoding.EncodeToString(expectedMAC)

	if !hmac.Equal([]byte(hmacHeader), []byte(expectedHMAC)) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid HMAC signature"})
	}

	return c.Next()
}

// HandleOrderPaid godoc
// @Summary Shopify Order Paid Webhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param request body ShopifyOrderWebhook true "Webhook Payload"
// @Success 200 "OK"
// @Router /webhooks/shopify/orders/paid [post]
func (h *Handler) HandleOrderPaid(c *fiber.Ctx) error {
	var payload ShopifyOrderWebhook
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return response.Error(c, err)
	}
	
	if err := h.service.HandleOrderPaid(c.Context(), payload); err != nil {
		return response.Error(c, err)
	}
	return c.SendStatus(fiber.StatusOK)
}

// HandleInventoryUpdate godoc
// @Summary Shopify Inventory Update Webhook
// @Tags Webhooks
// @Accept json
// @Produce json
// @Param request body ShopifyInventoryLevelWebhook true "Webhook Payload"
// @Success 200 "OK"
// @Router /webhooks/shopify/inventory_levels/update [post]
func (h *Handler) HandleInventoryUpdate(c *fiber.Ctx) error {
	var payload ShopifyInventoryLevelWebhook
	if err := json.Unmarshal(c.Body(), &payload); err != nil {
		return response.Error(c, err)
	}
	
	if err := h.service.HandleInventoryUpdate(c.Context(), payload); err != nil {
		return response.Error(c, err)
	}
	return c.SendStatus(fiber.StatusOK)
}
