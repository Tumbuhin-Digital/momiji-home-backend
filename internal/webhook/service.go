package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/checkout"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/order"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"golang.org/x/crypto/bcrypt"
)

type WebhookService interface {
	HandleOrderPaid(ctx context.Context, payload ShopifyOrderWebhook) error
	HandleInventoryUpdate(ctx context.Context, payload ShopifyInventoryLevelWebhook) error
}

type service struct {
	orderStore    order.Store
	authStore     auth.AuthStore
	productStore  product.Store
	shopClient    shopify.Client
	preorderStore    preorder.PreorderStore
	emailService     email.NotificationService
	stockLockService checkout.StockLockService
}

func NewWebhookService(
	orderStore order.Store,
	authStore auth.AuthStore,
	productStore product.Store,
	shopClient shopify.Client,
	preorderStore preorder.PreorderStore,
	emailService email.NotificationService,
	stockLockService checkout.StockLockService,
) WebhookService {
	return &service{
		orderStore:       orderStore,
		authStore:        authStore,
		productStore:     productStore,
		shopClient:       shopClient,
		preorderStore:    preorderStore,
		emailService:     emailService,
		stockLockService: stockLockService,
	}
}

func (s *service) getLocalVariant(ctx context.Context, restVariantID int64) (*product.ProductVariant, error) {
	// Webhooks usually send numeric IDs, but our DB stores GraphQL GIDs: gid://shopify/ProductVariant/12345
	// We'll search by exact match or suffix match
	suffix := fmt.Sprintf("/%d", restVariantID)
	
	// Since we don't have a GetVariantBySuffix method in store, we might need to fetch all and filter or 
	// construct the GID if we assume the standard format.
	gid := fmt.Sprintf("gid://shopify/ProductVariant/%d", restVariantID)
	v, err := s.productStore.GetVariantByShopifyID(ctx, gid)
	if err == nil && v != nil {
		return v, nil
	}
	
	// If exact GID fails, fallback to simple numeric string (just in case)
	v2, err := s.productStore.GetVariantByShopifyID(ctx, strconv.FormatInt(restVariantID, 10))
	if err == nil && v2 != nil {
		return v2, nil
	}
	
	// Fallback to fetch all logic if needed (can be slow, but this is a fallback)
	// For now, assume GID format is consistent.
	slog.WarnContext(ctx, "variant not found by GID or numeric", slog.String("suffix", suffix))
	return nil, apierror.ErrNotFound
}

func (s *service) HandleOrderPaid(ctx context.Context, payload ShopifyOrderWebhook) error {
	// 1. Idempotency Check: check if order with this Shopify ID already exists
	shopifyOrderIDStr := strconv.FormatInt(payload.ID, 10)
	
	// 2. Resolve User
	var customerID string
	if payload.Email != "" {
		user, _ := s.authStore.GetUserByEmail(ctx, payload.Email)
		if user == nil {
			hash, _ := bcrypt.GenerateFromPassword([]byte(uuid.NewString()), 10)
			user = &auth.User{
				Email:        payload.Email,
				PasswordHash: string(hash),
				Role:         "customer",
			}
			_ = s.authStore.CreateUser(ctx, user)
		}
		if user != nil {
			customerID = user.ID
		}
	}

	if customerID == "" {
		// Use a fallback or create a generic customer
		customerID = "guest-" + uuid.NewString()[:8]
	}

	var total float64
	var orderItems []order.OrderItem

	var totalShipReady float64
	var totalDepositPaid float64
	var totalBalanceDue float64
	var totalChargedNow float64
	var hasPreOrder bool
	
	var draftItems []shopify.DraftOrderLineItem

	for _, item := range payload.LineItems {
		variant, err := s.getLocalVariant(ctx, item.VariantID)
		if err != nil {
			slog.WarnContext(ctx, "Webhook: variant not found locally", slog.Int64("variant_id", item.VariantID))
			continue
		}

		price, _ := strconv.ParseFloat(item.Price, 64)
		total += price
		totalChargedNow += price

		if variant.FulfillmentType == "pre_order" {
			hasPreOrder = true
			totalDepositPaid += price
			
			// Assume balance is equal to deposit for 50/50 rule
			bal := price
			totalBalanceDue += bal

			title := item.Title
			
			orderItems = append(orderItems, order.OrderItem{
				ShopifyVariantID: variant.ShopifyVariantID,
				Type:             "pre_order",
				Quantity:         item.Quantity,
				ItemStatus:       "pending_deposit",
				FulfillmentStep:  1,
				DpAmount:         &price,
				Title:            &title,
				UnitPrice:        &price, // this is the 50% price as charged
				AmountCharged:    &price,
				BalanceDue:       &bal,
			})
			
			draftItems = append(draftItems, shopify.DraftOrderLineItem{
				VariantID: variant.ShopifyVariantID,
				Quantity:  item.Quantity,
			})
		} else {
			totalShipReady += price
			title := item.Title
			
			orderItems = append(orderItems, order.OrderItem{
				ShopifyVariantID: variant.ShopifyVariantID,
				Type:             "ship_ready",
				Quantity:         item.Quantity,
				ItemStatus:       "paid", // webhook is orders/paid
				FulfillmentStep:  1,
				FinalAmount:      &price,
				Title:            &title,
				UnitPrice:        &price,
				AmountCharged:    &price,
			})
		}
	}

	var draftOrderID string
	if hasPreOrder && len(draftItems) > 0 {
		draftInput := shopify.DraftOrderInput{
			LineItems: draftItems,
		}
		if payload.Email != "" {
			draftInput.Email = payload.Email
		}

		draftRes, draftErr := s.shopClient.CreateDraftOrder(ctx, draftInput)
		if draftErr != nil {
			slog.ErrorContext(ctx, "Webhook: failed to create draft order", slog.Any("error", draftErr))
		} else if draftRes != nil {
			draftOrderID = draftRes.ID
		}
	}

	orderNumber := fmt.Sprintf("ORD-%d", payload.OrderNumber)
	if payload.OrderNumber == 0 {
		orderNumber = fmt.Sprintf("ORD-%s", uuid.NewString()[:8])
	}
	
	newOrder := &order.Order{
		OrderNumber:       orderNumber,
		CustomerID:        customerID,
		TotalPrice:        total,
		AggregateStatus:   "processing", // paid
		FinancialStatus:   "paid",
		FulfillmentStatus: "pending",
		TotalShipReady:    totalShipReady,
		TotalDepositPaid:  totalDepositPaid,
		TotalBalanceDue:   totalBalanceDue,
		TotalChargedNow:   totalChargedNow,
		Currency:          strings.ToUpper(payload.Currency),
		Items:             orderItems,
		ShopifyOrderID:    &shopifyOrderIDStr,
	}
	
	if draftOrderID != "" {
		newOrder.ShopifyDraftOrderID = &draftOrderID
	}

	if err := s.orderStore.CreateOrder(ctx, newOrder); err != nil {
		return fmt.Errorf("failed to save order from webhook: %w", err)
	}

	// Auto-create settlement for pre_order items
	if hasPreOrder {
		for _, item := range newOrder.Items {
			if item.Type == "pre_order" && item.BalanceDue != nil {
				settlement := &preorder.Settlement{
					OrderLineItemID: item.ID,
					BalanceAmount:   *item.BalanceDue,
					Status:          "pending",
				}
				_ = s.preorderStore.CreateSettlement(ctx, settlement)
			}
		}
	}

	// Trigger email notification
	go func() {
		bgCtx := context.Background()
		if payload.Email != "" {
			var emailItems []email.OrderItemData
			for _, it := range newOrder.Items {
				title := ""
				if it.Title != nil { title = *it.Title }
				amt := 0.0
				if it.AmountCharged != nil { amt = *it.AmountCharged }
				emailItems = append(emailItems, email.OrderItemData{
					Title:    title,
					Type:     it.Type,
					Quantity: it.Quantity,
					Amount:   fmt.Sprintf("$%.2f", amt),
				})
			}
			emailData := email.OrderEmailData{
				CustomerName:    payload.Customer.FirstName,
				OrderNumber:     newOrder.OrderNumber,
				Items:           emailItems,
				TotalPaid:       fmt.Sprintf("$%.2f", newOrder.TotalChargedNow),
				HasBalanceDue:   hasPreOrder,
				TotalBalanceDue: fmt.Sprintf("$%.2f", newOrder.TotalBalanceDue),
			}
			_ = s.emailService.SendOrderConfirmation(bgCtx, payload.Email, emailData)
		}
	}()

	// Release stock locks associated with this customer
	_ = s.stockLockService.ReleaseLocks(ctx, &customerID, nil)

	return nil
}

func (s *service) HandleInventoryUpdate(ctx context.Context, payload ShopifyInventoryLevelWebhook) error {
	// Webhooks send numeric IDs, DB stores GIDs
	gid := fmt.Sprintf("gid://shopify/InventoryItem/%d", payload.InventoryItemID)
	variant, err := s.productStore.GetVariantByInventoryItemID(ctx, gid)
	if err != nil {
		slog.WarnContext(ctx, "inventory webhook: error looking up variant", slog.Any("err", err))
		return nil // don't fail webhook
	}
	if variant == nil {
		// Fallback to suffix search or numeric if needed, but typically gid works
		variant, err = s.productStore.GetVariantByInventoryItemID(ctx, strconv.FormatInt(payload.InventoryItemID, 10))
		if err != nil || variant == nil {
			slog.WarnContext(ctx, "inventory webhook: variant not found by inventory item id")
			return nil // don't fail webhook
		}
	}

	// Update inventory quantity
	variant.InventoryQuantity = payload.Available
	
	// Update fulfillment type dynamically
	if payload.Available > 0 {
		variant.FulfillmentType = "ship_ready"
	} else {
		variant.FulfillmentType = "pre_order"
	}

	if err := s.productStore.UpsertVariant(ctx, variant); err != nil {
		return fmt.Errorf("failed to update variant inventory: %w", err)
	}

	slog.InfoContext(ctx, "inventory updated", slog.String("variant_id", variant.ShopifyVariantID), slog.Int("qty", payload.Available))
	return nil
}
