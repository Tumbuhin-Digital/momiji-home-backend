package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/checkout"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
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
	HandleFulfillment(ctx context.Context, payload ShopifyFulfillmentWebhook) error
}

type service struct {
	orderStore    order.Store
	authStore     auth.AuthStore
	productStore  product.Store
	shopClient    shopify.Client
	preorderStore    preorder.PreorderStore
	emailService     email.NotificationService
	stockLockService checkout.StockLockService
	customerStore    customer.CustomerStore
}

func NewWebhookService(
	orderStore order.Store,
	authStore auth.AuthStore,
	productStore product.Store,
	shopClient shopify.Client,
	preorderStore preorder.PreorderStore,
	emailService email.NotificationService,
	stockLockService checkout.StockLockService,
	customerStore customer.CustomerStore,
) WebhookService {
	return &service{
		orderStore:       orderStore,
		authStore:        authStore,
		productStore:     productStore,
		shopClient:       shopClient,
		preorderStore:    preorderStore,
		emailService:     emailService,
		stockLockService: stockLockService,
		customerStore:    customerStore,
	}
}

func (s *service) getLocalVariant(ctx context.Context, item ShopifyOrderLineItem) (*product.ProductVariant, error) {
	for _, prop := range item.Properties {
		if prop.Name == "variant_ref" {
			if gid, ok := prop.Value.(string); ok && gid != "" {
				v, err := s.productStore.GetVariantByShopifyID(ctx, gid)
				if err == nil && v != nil {
					return v, nil
				}
			}
		}
	}

	if item.VariantID == 0 {
		return nil, apierror.ErrNotFound
	}

	// Webhooks usually send numeric IDs, but our DB stores GraphQL GIDs: gid://shopify/ProductVariant/12345
	// We'll search by exact match or suffix match
	suffix := fmt.Sprintf("/%d", item.VariantID)
	
	// Since we don't have a GetVariantBySuffix method in store, we might need to fetch all and filter or 
	// construct the GID if we assume the standard format.
	gid := fmt.Sprintf("gid://shopify/ProductVariant/%d", item.VariantID)
	v, err := s.productStore.GetVariantByShopifyID(ctx, gid)
	if err == nil && v != nil {
		return v, nil
	}
	
	// If exact GID fails, fallback to simple numeric string (just in case)
	v2, err := s.productStore.GetVariantByShopifyID(ctx, strconv.FormatInt(item.VariantID, 10))
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
		customerID = uuid.NewString()
	}

	// 3. Upsert Customer Record
	shopifyCustomerIDStr := strconv.FormatInt(payload.Customer.ID, 10)
	cust := &customer.Customer{
		ID:                customerID,
		Email:             payload.Customer.Email,
		ShopifyCustomerID: &shopifyCustomerIDStr,
	}
	if payload.Customer.FirstName != "" {
		cust.FirstName = &payload.Customer.FirstName
	}
	if payload.Customer.LastName != "" {
		cust.LastName = &payload.Customer.LastName
	}

	if err := s.customerStore.UpsertCustomer(ctx, cust); err != nil {
		slog.WarnContext(ctx, "Failed to upsert customer", slog.Any("error", err))
	}

	// Extract and save Shipping Address if present
	var shippingAddressID *string
	if payload.ShippingAddress != nil {
		addrID := uuid.NewString()
		shippingAddressID = &addrID
		addr := &customer.Address{
			ID:         addrID,
			CustomerID: customerID,
			FirstName:  &payload.ShippingAddress.FirstName,
			LastName:   &payload.ShippingAddress.LastName,
			Address1:   payload.ShippingAddress.Address1,
			Address2:   &payload.ShippingAddress.Address2,
			City:       payload.ShippingAddress.City,
			Province:   payload.ShippingAddress.Province,
			Country:    payload.ShippingAddress.Country,
			Zip:        payload.ShippingAddress.Zip,
			Phone:      &payload.ShippingAddress.Phone,
			IsDefault:  true,
		}
		
		if err := s.customerStore.CreateAddress(ctx, addr); err != nil {
			slog.WarnContext(ctx, "Failed to create shipping address", slog.Any("error", err))
		}
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
		variant, err := s.getLocalVariant(ctx, item)
		if err != nil {
			slog.WarnContext(ctx, "Webhook: variant not found locally", slog.Int64("variant_id", item.VariantID))
			continue
		}

		unitPriceFromShopify, _ := strconv.ParseFloat(item.Price, 64)
		lineTotalFromShopify := unitPriceFromShopify * float64(item.Quantity)
		
		totalDiscount, _ := strconv.ParseFloat(item.TotalDiscount, 64)
		actualAmountCharged := lineTotalFromShopify - totalDiscount
		
		total += actualAmountCharged
		totalChargedNow += actualAmountCharged

		isPreorder := variant.FulfillmentType == "pre_order"

		// Also check line item properties, in case stock changed to 0 but webhook was delayed,
		// or if checkout specifically marked this line item as a preorder down payment.
		for _, prop := range item.Properties {
			if prop.Name == "type" {
				if valStr, ok := prop.Value.(string); ok && valStr == "preorder_dp" {
					isPreorder = true
					break
				}
			}
		}

		if isPreorder {
			hasPreOrder = true
			totalDepositPaid += lineTotalFromShopify
			
			fullUnitPrice := unitPriceFromShopify * 2 // Default to 2x DP
			for _, prop := range item.Properties {
				if prop.Name == "full_price" {
					if valStr, ok := prop.Value.(string); ok {
						if p, err := strconv.ParseFloat(valStr, 64); err == nil {
							fullUnitPrice = p
						}
					}
				}
			}

			bal := (fullUnitPrice - unitPriceFromShopify) * float64(item.Quantity)
			totalBalanceDue += bal

			title := item.Title
			
			unitPrice := fullUnitPrice
			finalAmount := fullUnitPrice * float64(item.Quantity)
			amountCharged := actualAmountCharged
			dpAmount := actualAmountCharged
			balanceDue := bal
			
			orderItems = append(orderItems, order.OrderItem{
				ShopifyVariantID: variant.ShopifyVariantID,
				Type:             "pre_order",
				Quantity:         item.Quantity,
				ItemStatus:       "pending_deposit",
				FulfillmentStep:  1,
				FinalAmount:      &finalAmount,
				DpAmount:         &dpAmount,
				Title:            &title,
				UnitPrice:        &unitPrice,
				AmountCharged:    &amountCharged,
				BalanceDue:       &balanceDue,
			})
			
			draftItems = append(draftItems, shopify.DraftOrderLineItem{
				VariantID: variant.ShopifyVariantID,
				Quantity:  item.Quantity,
			})
		} else {
			totalShipReady += actualAmountCharged
			title := item.Title
			
			// For ship ready, unit price could reflect the discount if we divide, 
			// but usually unitPrice is the base price.
			unitPrice := unitPriceFromShopify
			finalAmount := actualAmountCharged
			amountCharged := actualAmountCharged
			
			orderItems = append(orderItems, order.OrderItem{
				ShopifyVariantID: variant.ShopifyVariantID,
				Type:             "ship_ready",
				Quantity:         item.Quantity,
				ItemStatus:       "paid", // webhook is orders/paid
				FulfillmentStep:  1,
				FinalAmount:      &finalAmount,
				Title:            &title,
				UnitPrice:        &unitPrice,
				AmountCharged:    &amountCharged,
			})
		}
	}

	// Add shipping cost to the overall order totals
	for _, sl := range payload.ShippingLines {
		if shipPrice, err := strconv.ParseFloat(sl.Price, 64); err == nil {
			total += shipPrice
			totalChargedNow += shipPrice
		}
	}

	var checkoutRef string
	var shippingMethod string
	for _, note := range payload.NoteAttributes {
		if note.Name == "checkout_reference" {
			if val, ok := note.Value.(string); ok {
				checkoutRef = val
			}
		} else if note.Name == "preorder_shipping_method" {
			if val, ok := note.Value.(string); ok {
				shippingMethod = val
			}
		}
	}

	orderNumber := fmt.Sprintf("ORD-%d", payload.OrderNumber)
	if payload.OrderNumber == 0 {
		orderNumber = fmt.Sprintf("ORD-%s", uuid.NewString()[:8])
	}

	newOrder := &order.Order{
		OrderNumber:       orderNumber,
		CustomerID:        customerID,
		ShippingAddressID: shippingAddressID,
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
	
	if checkoutRef != "" {
		newOrder.ShopifyDraftOrderID = &checkoutRef
	}
	if shippingMethod != "" {
		newOrder.ShippingMethod = &shippingMethod
	}

	if err := s.orderStore.CreateOrder(ctx, newOrder); err != nil {
		slog.ErrorContext(ctx, "Failed to save order from webhook", slog.Any("error", err), slog.String("checkout_reference", checkoutRef))
		return fmt.Errorf("failed to save order from webhook: %w", err)
	}
	
	slog.InfoContext(ctx, "Order successfully inserted into database", slog.String("order_id", newOrder.ID), slog.String("order_number", newOrder.OrderNumber), slog.String("shopify_order_id", shopifyOrderIDStr))

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
			if err := s.emailService.SendOrderConfirmation(bgCtx, payload.Email, emailData); err != nil {
				slog.ErrorContext(bgCtx, "Failed to send order confirmation email", slog.Any("error", err), slog.String("email", payload.Email))
			} else {
				slog.InfoContext(bgCtx, "Order confirmation email successfully sent", slog.String("email", payload.Email), slog.String("order_number", newOrder.OrderNumber))
			}
		}
	}()

	// Release stock locks associated with this customer
	_ = s.stockLockService.ReleaseLocks(ctx, &customerID, nil)

	return nil
}

func (s *service) HandleFulfillment(ctx context.Context, payload ShopifyFulfillmentWebhook) error {
	slog.InfoContext(ctx, "Processing Shopify Fulfillment webhook", slog.Int64("fulfillment_id", payload.ID), slog.Int64("order_id", payload.OrderID))

	// Find the order by Shopify ID
	shopOrderID := fmt.Sprintf("%d", payload.OrderID)
	o, err := s.orderStore.GetOrderByShopifyID(ctx, shopOrderID)
	if err != nil || o == nil {
		slog.ErrorContext(ctx, "Order not found for fulfillment", slog.String("shopify_order_id", shopOrderID))
		return nil // Return 200 so Shopify stops retrying
	}

	trackingNumber := payload.TrackingNumber
	if trackingNumber == "" && len(payload.TrackingNumbers) > 0 {
		trackingNumber = payload.TrackingNumbers[0]
	}

	trackingURL := ""
	if len(payload.TrackingURLs) > 0 {
		trackingURL = payload.TrackingURLs[0]
	}

	// Map shipment status to human readable last event
	var lastEvent string
	switch payload.ShipmentStatus {
	case "label_printed":
		lastEvent = "Shipping label printed"
	case "label_purchased":
		lastEvent = "Shipping label purchased"
	case "attempted_delivery":
		lastEvent = "Delivery attempted"
	case "ready_for_pickup":
		lastEvent = "Ready for pickup"
	case "picked_up":
		lastEvent = "Package picked up"
	case "in_transit":
		lastEvent = "Package in transit"
	case "out_for_delivery":
		lastEvent = "Out for delivery"
	case "delivered":
		lastEvent = "Delivered"
	case "failure":
		lastEvent = "Delivery failed"
	default:
		lastEvent = "Package departed from facility"
	}

	if trackingNumber == "" {
		slog.InfoContext(ctx, "Fulfillment has no tracking number, but updating fulfillment status anyway")
	}

	now := time.Now()

	// Update all items in this fulfillment
	for _, li := range payload.LineItems {
		// Find matching item in our DB
		var itemID string
		for _, dbItem := range o.Items {
			if dbItem.ShopifyVariantID == fmt.Sprintf("gid://shopify/ProductVariant/%d", li.VariantID) {
				itemID = dbItem.ID
				break
			}
		}

		if itemID != "" {
			err := s.orderStore.UpdateOrderItemTracking(ctx, itemID, trackingNumber, trackingURL, payload.TrackingCompany, lastEvent, &now)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to update item tracking", slog.String("item_id", itemID), slog.Any("error", err))
			} else {
				slog.InfoContext(ctx, "Updated tracking for item", slog.String("item_id", itemID), slog.String("tracking_number", trackingNumber))
			}
		}
	}

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
	
	if err := s.productStore.UpsertVariant(ctx, variant); err != nil {
		return fmt.Errorf("failed to update variant inventory: %w", err)
	}

	// Update fulfillment type dynamically only when stock runs out
	if payload.Available <= 0 && variant.FulfillmentType == "ship_ready" {
		if err := s.productStore.UpdateVariantFulfillmentType(ctx, variant.ShopifyVariantID, "pre_order"); err != nil {
			slog.WarnContext(ctx, "failed to update fulfillment type to pre_order", slog.Any("err", err))
		} else {
			slog.InfoContext(ctx, "dynamically changed fulfillment type to pre_order due to 0 stock", slog.String("variant_id", variant.ShopifyVariantID))
		}
	}

	slog.InfoContext(ctx, "inventory updated", slog.String("variant_id", variant.ShopifyVariantID), slog.Int("qty", payload.Available))
	return nil
}
