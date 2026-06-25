package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"

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
	webhooks.Post("/orders/paid", h.withDedupe("orders/paid", h.HandleOrderPaid))
	webhooks.Post("/inventory_levels/update", h.withDedupe("inventory_levels/update", h.HandleInventoryUpdate))
	webhooks.Post("/fulfillments/create", h.withDedupe("fulfillments/create", h.HandleFulfillment))
	webhooks.Post("/fulfillments/update", h.withDedupe("fulfillments/update", h.HandleFulfillment))
}

func (h *Handler) withDedupe(topic string, handler fiber.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		webhookID := c.Get("X-Shopify-Webhook-Id")
		if webhookID != "" {
			already, err := h.service.IsWebhookProcessed(c.Context(), webhookID)
			if err != nil {
				slog.WarnContext(c.Context(), "webhook dedupe check failed", slog.Any("error", err))
			} else if already {
				slog.InfoContext(c.Context(), "skipping duplicate webhook", slog.String("webhook_id", webhookID), slog.String("topic", topic))
				return c.SendStatus(fiber.StatusOK)
			}
		}
		if err := handler(c); err != nil {
			return err
		}
		if webhookID != "" {
			if err := h.service.SaveWebhookEvent(c.Context(), webhookID, topic); err != nil {
				slog.WarnContext(c.Context(), "failed to save webhook event", slog.Any("error", err))
			}
		}
		return nil
	}
}

func (h *Handler) verifyShopifyHMAC(c *fiber.Ctx) error {
	hmacHeader := c.Get("X-Shopify-Hmac-Sha256")
	if hmacHeader == "" {
		slog.WarnContext(c.Context(), "Webhook rejected: Missing HMAC header")
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Missing HMAC header"})
	}

	body := c.Body()
	
	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)
	expectedHMAC := base64.StdEncoding.EncodeToString(expectedMAC)

	if !hmac.Equal([]byte(hmacHeader), []byte(expectedHMAC)) {
		slog.WarnContext(c.Context(), "Webhook rejected: Invalid HMAC signature", slog.String("expected", expectedHMAC), slog.String("got", hmacHeader))
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
		slog.ErrorContext(c.Context(), "Failed to unmarshal webhook payload", slog.Any("error", err))
		return response.Error(c, err)
	}
	
	slog.InfoContext(c.Context(), "Received orders/paid webhook", slog.Int64("order_id", payload.ID), slog.Int("order_number", payload.OrderNumber))

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

// @Summary Handle Shopify Fulfillment Webhook
// @Tags Webhooks
// @Accept json
// @Router /webhooks/shopify/fulfillments/create [post]
// @Router /webhooks/shopify/fulfillments/update [post]
func (h *Handler) HandleFulfillment(c *fiber.Ctx) error {
	var payload ShopifyFulfillmentWebhook
	if err := c.BodyParser(&payload); err != nil {
		slog.ErrorContext(c.Context(), "Failed to parse fulfillment webhook", slog.Any("error", err))
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid payload"})
	}

	if err := h.service.HandleFulfillment(c.Context(), payload); err != nil {
		slog.ErrorContext(c.Context(), "Failed to process fulfillment webhook", slog.Any("error", err))
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Internal server error"})
	}

	return c.SendStatus(fiber.StatusOK)
}
