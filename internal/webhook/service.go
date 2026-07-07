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
	HandleProductDelete(ctx context.Context, payload ShopifyProductDeleteWebhook) error
	IsWebhookProcessed(ctx context.Context, webhookID string) (bool, error)
	SaveWebhookEvent(ctx context.Context, webhookID, topic string) error
}

type FulfillmentSyncer interface {
	SyncFulfillmentOrdersFromShopify(ctx context.Context, orderID, shopifyOrderID string) error
}

type service struct {
	orderStore       order.Store
	authStore        auth.AuthStore
	productStore     product.Store
	shopClient       shopify.Client
	preorderStore    preorder.PreorderStore
	emailService     email.NotificationService
	stockLockService checkout.StockLockService
	customerStore    customer.CustomerStore
	fulfillmentSync  FulfillmentSyncer
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
	fulfillmentSync FulfillmentSyncer,
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
		fulfillmentSync:  fulfillmentSync,
	}
}

func (s *service) IsWebhookProcessed(ctx context.Context, webhookID string) (bool, error) {
	return s.orderStore.IsWebhookProcessed(ctx, webhookID)
}

func (s *service) SaveWebhookEvent(ctx context.Context, webhookID, topic string) error {
	return s.orderStore.SaveWebhookEvent(ctx, webhookID, topic)
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

	// Extract and save shipping address (Shopify payload, note attribute, or billing fallback).
	resolvedAddr, addrSource := resolveOrderShippingAddress(ctx, payload)
	if addrSource != "" {
		slog.InfoContext(ctx, "resolved shipping address for order",
			slog.String("source", addrSource))
	}
	shippingAddressID := createCustomerShippingAddress(
		ctx,
		s.customerStore,
		customerID,
		resolvedAddr,
	)

	var total float64
	var orderItems []order.OrderItem

	var totalShipReady float64
	var totalDepositPaid float64
	var totalBalanceDue float64
	var totalChargedNow float64
	var hasPreOrder bool
	
	var draftItems []shopify.DraftOrderLineItem
	var settlementIDsPaid []string
	var settlementOrderID string

	for _, item := range payload.LineItems {
		var settlementID string
		for _, prop := range item.Properties {
			if prop.Name == "settlement_id" {
				if valStr, ok := prop.Value.(string); ok {
					settlementID = valStr
					break
				}
			}
		}

		if settlementID != "" {
			slog.InfoContext(ctx, "Webhook: Received payment for settlement", slog.String("settlement_id", settlementID))

			now := time.Now()
			_ = s.preorderStore.UpdateSettlementStatus(ctx, settlementID, "paid", &now)

			st, err := s.preorderStore.GetSettlementByID(ctx, settlementID)
			if err == nil {
				_ = s.orderStore.UpdateItemStatusByID(ctx, st.OrderLineItemID, "paid")
				if settlementOrderID == "" {
					settlementOrderID = st.OrderID
				}
			}
			settlementIDsPaid = append(settlementIDsPaid, settlementID)
			continue
		}

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

		isPreorder := isPreorderShopifyLineItem(item)

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

			unitPrice := unitPriceFromShopify
			if variant.WSPrice != nil && *variant.WSPrice > 0 {
				unitPrice = *variant.WSPrice
			} else if item.Quantity > 0 && actualAmountCharged > 0 {
				unitPrice = actualAmountCharged / float64(item.Quantity)
			}

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

	if len(orderItems) == 0 {
		if len(settlementIDsPaid) > 0 && settlementOrderID != "" {
			return s.handleSettlementOnlyPayment(ctx, settlementOrderID)
		}
		slog.InfoContext(ctx, "Webhook: No new valid items to create an order for", slog.String("shopify_order_id", shopifyOrderIDStr))
		return nil
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
	var preorderShippingEstimate string
	var preorderWarehouseOrigin string
	for _, note := range payload.NoteAttributes {
		if note.Name == "checkout_reference" {
			if val, ok := note.Value.(string); ok {
				checkoutRef = val
			}
		} else if note.Name == "preorder_shipping_method" {
			if val, ok := note.Value.(string); ok {
				shippingMethod = val
			}
		} else if note.Name == "preorder_shipping_estimate" {
			if val, ok := note.Value.(string); ok {
				preorderShippingEstimate = val
			}
		} else if note.Name == "preorder_warehouse_origin" {
			if val, ok := note.Value.(string); ok {
				preorderWarehouseOrigin = val
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

		var preItems []order.OrderItem
		var variantIDs []string
		for _, item := range newOrder.Items {
			if item.Type == "pre_order" {
				preItems = append(preItems, item)
				variantIDs = append(variantIDs, item.ShopifyVariantID)
			}
		}
		if len(preItems) > 0 {
			var estimate *float64
			if preorderShippingEstimate != "" {
				if v, err := strconv.ParseFloat(preorderShippingEstimate, 64); err == nil {
					estimate = &v
				} else {
					slog.WarnContext(ctx, "invalid pre-order shipping estimate from checkout",
						slog.String("order_id", newOrder.ID),
						slog.String("value", preorderShippingEstimate))
				}
			} else {
				slog.WarnContext(ctx, "pre-order checkout shipping estimate missing",
					slog.String("order_id", newOrder.ID))
			}

			dims, dimErr := s.orderStore.GetVariantDimensions(ctx, variantIDs)
			if dimErr != nil {
				slog.ErrorContext(ctx, "failed to load variant dimensions for pre-order shipment",
					slog.String("order_id", newOrder.ID),
					slog.Any("error", dimErr))
			} else {
				shipment, packing, buildErr := order.BuildCheckoutPreorderShipment(newOrder.ID, preItems, dims, estimate, preorderWarehouseOrigin)
				if buildErr != nil {
					slog.ErrorContext(ctx, "failed to build pre-order shipment from checkout packing",
						slog.String("order_id", newOrder.ID),
						slog.Any("error", buildErr))
				} else if err := s.orderStore.UpsertPreorderShipment(ctx, shipment, packing); err != nil {
					slog.ErrorContext(ctx, "failed to create pre-order shipment from checkout",
						slog.String("order_id", newOrder.ID),
						slog.Any("error", err))
				}
			}
		}
	}

	// Shopify automatically sends order confirmations, so we skip sending our own here.

	// Release stock locks associated with this checkout attempt
	if checkoutRef != "" {
		_ = s.stockLockService.ReleaseLocksByCheckoutReference(ctx, checkoutRef)
	} else {
		_ = s.stockLockService.ReleaseLocks(ctx, &customerID, nil)
	}

	if s.fulfillmentSync != nil && newOrder.ShopifyOrderID != nil {
		_ = s.fulfillmentSync.SyncFulfillmentOrdersFromShopify(ctx, newOrder.ID, *newOrder.ShopifyOrderID)
	}

	return nil
}

func (s *service) HandleFulfillment(ctx context.Context, payload ShopifyFulfillmentWebhook) error {
	slog.InfoContext(ctx, "Processing Shopify Fulfillment webhook", slog.Int64("fulfillment_id", payload.ID), slog.Int64("order_id", payload.OrderID))

	shopOrderID := fmt.Sprintf("%d", payload.OrderID)
	o, err := s.orderStore.GetOrderByShopifyID(ctx, shopOrderID)
	if err != nil || o == nil {
		slog.ErrorContext(ctx, "Order not found for fulfillment", slog.String("shopify_order_id", shopOrderID))
		return nil
	}

	trackingNumber := payload.TrackingNumber
	if trackingNumber == "" && len(payload.TrackingNumbers) > 0 {
		trackingNumber = payload.TrackingNumbers[0]
	}
	trackingURL := ""
	if len(payload.TrackingURLs) > 0 {
		trackingURL = payload.TrackingURLs[0]
	}

	var lastEvent string
	itemStatus := "shipped"
	shipmentStatus := payload.ShipmentStatus

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
		itemStatus = "delivered"
	case "failure":
		lastEvent = "Delivery failed"
	default:
		lastEvent = "Package departed from facility"
	}

	if trackingNumber == "" && payload.Status == "success" {
		lastEvent = "Delivered"
		itemStatus = "delivered"
		shipmentStatus = "delivered"
	}

	now := time.Now()
	shopifyFID := fmt.Sprintf("gid://shopify/Fulfillment/%d", payload.ID)
	fulfillmentStatus := "fulfilled"
	if itemStatus == "delivered" {
		fulfillmentStatus = "delivered"
	}

	const (
		shipReadyStepShipped   = 3
		shipReadyStepDelivered = 4
		preOrderStepShipped    = 4
		preOrderStepDelivered  = 5
	)

	var flis []order.FulfillmentLineItem
	var preOrderItemIDs []string

	for _, li := range payload.LineItems {
		variantGID := fmt.Sprintf("gid://shopify/ProductVariant/%d", li.VariantID)
		var itemID string
		var dbItem order.OrderItem
		for _, it := range o.Items {
			if it.ShopifyVariantID != variantGID {
				continue
			}
			if it.Title != nil && *it.Title != "" && li.Title != "" && *it.Title != li.Title {
				continue
			}
			itemID = it.ID
			dbItem = it
			break
		}
		if itemID == "" {
			continue
		}

		qty := li.Quantity
		if qty <= 0 {
			qty = 1
		}

		var step int
		switch dbItem.Type {
		case "pre_order":
			flis = append(flis, order.FulfillmentLineItem{
				OrderLineItemID: itemID,
				Quantity:        qty,
			})
			preOrderItemIDs = append(preOrderItemIDs, itemID)
			step = preOrderStepShipped
			if itemStatus == "delivered" {
				step = preOrderStepDelivered
			}
		case "ship_ready":
			step = shipReadyStepShipped
			if itemStatus == "delivered" {
				step = shipReadyStepDelivered
			}
		default:
			continue
		}

		if err := s.orderStore.UpdateOrderItemTracking(ctx, itemID, trackingNumber, trackingURL, payload.TrackingCompany, lastEvent, itemStatus, step, &now); err != nil {
			slog.WarnContext(ctx, "Failed to update line item tracking from fulfillment webhook", slog.String("item_id", itemID), slog.Any("error", err))
			continue
		}

		for i, it := range o.Items {
			if it.ID == itemID {
				o.Items[i].ItemStatus = itemStatus
				o.Items[i].FulfillmentStep = step
				break
			}
		}
	}

	if len(flis) > 0 {
		seq, _ := s.orderStore.GetNextFulfillmentSequence(ctx, o.ID)
		tn := trackingNumber
		tu := trackingURL
		tc := payload.TrackingCompany
		ss := shipmentStatus
		f := &order.Fulfillment{
			OrderID:              o.ID,
			ShopifyFulfillmentID: &shopifyFID,
			SequenceNumber:       seq,
			TrackingNumber:       &tn,
			TrackingURL:          &tu,
			TrackingCompany:      &tc,
			ShipmentStatus:       &ss,
			Status:               fulfillmentStatus,
			FulfilledAt:          &now,
		}
		if itemStatus == "delivered" {
			f.DeliveredAt = &now
		}
		if err := s.orderStore.UpsertFulfillmentByShopifyID(ctx, f, flis); err != nil {
			slog.ErrorContext(ctx, "Failed to upsert fulfillment from webhook", slog.Any("error", err))
		}

		for _, itemID := range preOrderItemIDs {
			folis, _ := s.orderStore.GetFOLIByOrderLineItemIDs(ctx, o.ID, []string{itemID})
			for _, foli := range folis {
				qty := 0
				for _, fli := range flis {
					if fli.OrderLineItemID == itemID {
						qty = fli.Quantity
						break
					}
				}
				if qty > 0 {
					_ = s.orderStore.DecrementFOLIRemaining(ctx, foli.ID, qty)
				}
			}
		}
	}

	allDelivered := len(o.Items) > 0
	for _, it := range o.Items {
		if it.ItemStatus != "delivered" {
			allDelivered = false
			break
		}
	}

	if allDelivered {
		if err := s.orderStore.UpdateOrderStatus(ctx, o.ID, "completed", o.FinancialStatus, "fulfilled"); err != nil {
			slog.ErrorContext(ctx, "Failed to update master order status to completed during Shopify fulfillment", slog.String("order_id", o.ID), slog.Any("error", err))
		}
	} else if o.AggregateStatus != "completed" {
		if err := s.orderStore.UpdateOrderStatus(ctx, o.ID, "on_progress", o.FinancialStatus, "in_progress"); err != nil {
			slog.ErrorContext(ctx, "Failed to update master order status during Shopify fulfillment", slog.String("order_id", o.ID), slog.Any("error", err))
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

func (s *service) HandleProductDelete(ctx context.Context, payload ShopifyProductDeleteWebhook) error {
	if payload.ID == 0 {
		slog.WarnContext(ctx, "product delete webhook skipped: missing product id")
		return nil
	}

	shopifyProductID := strconv.FormatInt(payload.ID, 10)
	if err := s.productStore.MarkProductDeletedByShopifyID(ctx, shopifyProductID); err != nil {
		return fmt.Errorf("failed to mark product deleted by shopify id %s: %w", shopifyProductID, err)
	}

	slog.InfoContext(ctx, "product marked deleted from Shopify webhook",
		slog.String("shopify_product_id", shopifyProductID))
	return nil
}

func (s *service) handleSettlementOnlyPayment(ctx context.Context, orderID string) error {
	allPaid, err := s.preorderStore.AllSettlementsPaid(ctx, orderID)
	if err != nil {
		return err
	}
	if !allPaid {
		slog.InfoContext(ctx, "Webhook: partial settlement payment received", slog.String("order_id", orderID))
		return nil
	}

	if err := s.orderStore.UpdateOrderStatus(ctx, orderID, "on_progress", "paid", "in_progress"); err != nil {
		return err
	}
	if err := s.orderStore.UpdateItemStatusByType(ctx, orderID, "pre_order", "payment_received"); err != nil {
		return err
	}
	if err := s.orderStore.UpdateItemStepByType(ctx, orderID, "pre_order", 4); err != nil {
		return err
	}

	slog.InfoContext(ctx, "Webhook: all settlements paid, advanced to step 4", slog.String("order_id", orderID))

	if s.fulfillmentSync != nil {
		o, err := s.orderStore.GetOrder(ctx, orderID, "")
		if err == nil && o != nil && o.ShopifyOrderID != nil {
			_ = s.fulfillmentSync.SyncFulfillmentOrdersFromShopify(ctx, orderID, *o.ShopifyOrderID)
		}
	}

	// Best-effort settlement paid email — load order for customer email
	o, err := s.orderStore.GetOrder(ctx, orderID, "")
	if err != nil || o == nil || o.Customer == nil {
		return nil
	}

	user, _ := s.authStore.GetUserByID(ctx, o.CustomerID)
	if user == nil {
		return nil
	}

	customerName := "Customer"
	if o.Customer.FirstName != nil {
		customerName = *o.Customer.FirstName
	}

	go func() {
		bgCtx := context.Background()
		_ = s.emailService.SendSettlementPaid(bgCtx, user.Email, email.SettlementEmailData{
			CustomerName:  customerName,
			ItemTitle:     o.OrderNumber,
			BalanceAmount: fmt.Sprintf("$%.2f", o.TotalBalanceDue),
			TotalDue:      fmt.Sprintf("$%.2f", o.TotalBalanceDue),
		})
	}()

	return nil
}
