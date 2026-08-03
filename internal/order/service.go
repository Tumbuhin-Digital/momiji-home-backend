package order

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"strings"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorderbatch"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"
)

type OrderService interface {
	CreateOrder(ctx context.Context, userID, sessionID *string, req CreateOrderRequest) (*OrderResponse, error)
	GetOrders(ctx context.Context, userID string, query OrderQuery) ([]OrderResponse, int64, error)
	GetOrder(ctx context.Context, userID, orderID string) (*OrderResponse, error)
	GetOrderByShopifyID(ctx context.Context, shopifyOrderID string) (*OrderResponse, error)
	AcceptOrder(ctx context.Context, userID, orderID, fulfillmentType string) error
	CancelOrder(ctx context.Context, userID, orderID, fulfillmentType, reason string) error
	UpdateFulfillmentStep(ctx context.Context, userID, orderID, itemID string, step int) error
	UpdateItemsReceived(ctx context.Context, userID, orderID string, items []UpdateReceivedItem) error
	AddTrackingNumber(ctx context.Context, userID, orderID string, itemIDs []string, trackingNumber, trackingURL string) error
	CreatePreorderFulfillment(ctx context.Context, userID, orderID string, req CreateFulfillmentRequest) (*FulfillmentDTO, error)
	MarkFulfillmentDelivered(ctx context.Context, userID, orderID, fulfillmentID string) error
	SyncFulfillmentOrdersFromShopify(ctx context.Context, orderID, shopifyOrderID string) error
	GetOrderTracking(ctx context.Context, userID, orderID string) ([]shipstation.TrackingResponse, error)
	ExportOrdersToExcel(ctx context.Context, query OrderQuery) ([]byte, error)
	CalculatePreorderShipping(ctx context.Context, userID, orderID string, req CalculatePreorderShippingRequest) (*CalculatePreorderShippingResponse, error)
	UpdatePreorderShipping(ctx context.Context, userID, orderID string, req UpdatePreorderShippingRequest) (*PreorderShipmentDTO, error)
	RequestSecondPayment(ctx context.Context, userID, orderID string, req RequestSecondPaymentRequest) error
}

type service struct {
	store             Store
	cartService       cart.CartService
	authStore         auth.AuthStore
	shopClient        shopify.Client
	shipstationClient shipstation.Client
	shipstationCfg    config.ShipStationConfig
	warehouseResolver warehouse.Resolver
	preorderStore     preorder.PreorderStore
	preorderService   preorder.PreorderService
	emailService      email.NotificationService
	batchService      BatchAllocationService
}

type BatchAllocationService interface {
	ReleaseAllocationsByOrderLineItemID(ctx context.Context, orderLineItemID string) error
	GetCommittedAllocationsByOrderLineItemIDs(ctx context.Context, orderLineItemIDs []string) ([]preorderbatch.BatchAllocation, error)
	GetBatchesByIDs(ctx context.Context, batchIDs []string) ([]preorderbatch.PreorderBatch, error)
}

// BatchAllocationReleaser is kept for wiring compatibility.
type BatchAllocationReleaser = BatchAllocationService

func NewOrderService(store Store, cartService cart.CartService, authStore auth.AuthStore, shopClient shopify.Client,
	preorderStore preorder.PreorderStore,
	preorderService preorder.PreorderService,
	notificationService email.NotificationService,
	shipstationClient shipstation.Client,
	shipstationCfg config.ShipStationConfig,
	warehouseResolver warehouse.Resolver,
) OrderService {
	return &service{
		store:             store,
		cartService:       cartService,
		authStore:         authStore,
		shopClient:        shopClient,
		shipstationClient: shipstationClient,
		shipstationCfg:    shipstationCfg,
		warehouseResolver: warehouseResolver,
		preorderStore:     preorderStore,
		preorderService:   preorderService,
		emailService:      notificationService,
	}
}

func (s *service) SetBatchAllocationReleaser(batchService BatchAllocationService) {
	s.batchService = batchService
}

func (s *service) CreateOrder(ctx context.Context, userID, sessionID *string, req CreateOrderRequest) (*OrderResponse, error) {
	// 1. Resolve User
	var customerID string
	if userID != nil && *userID != "" {
		customerID = *userID
	} else if req.GuestInfo != nil {
		user, _ := s.authStore.GetUserByEmail(ctx, req.GuestInfo.Email)
		if user == nil {
			hash, _ := bcrypt.GenerateFromPassword([]byte(uuid.NewString()), 10)
			user = &auth.User{
				Email:        req.GuestInfo.Email,
				PasswordHash: string(hash),
				Role:         "customer",
			}
			if err := s.authStore.CreateUser(ctx, user); err != nil {
				return nil, apierror.ErrInternal
			}
		}
		customerID = user.ID
	} else {
		return nil, apierror.ErrUnauthorized
	}

	// 2. Fetch Cart
	cartRes, err := s.cartService.GetCartResponse(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if len(cartRes.ShipReady) == 0 && len(cartRes.PreOrder) == 0 {
		return nil, apierror.ErrBadRequest
	}

	var total float64
	var orderItems []OrderItem

	var checkoutURL string
	var draftInvoiceURL string

	var totalShipReady float64
	var totalDepositPaid float64
	var totalBalanceDue float64
	var totalChargedNow float64

	var shippingCost float64
	if req.ShippingPrice != "" {
		shippingCost, _ = strconv.ParseFloat(req.ShippingPrice, 64)
	}

	// 3. Process ShipReady (Storefront Checkout)
	if len(cartRes.ShipReady) > 0 {
		var sfItems []shopify.CartLineInput
		for _, item := range cartRes.ShipReady {
			sfItems = append(sfItems, shopify.CartLineInput{
				MerchandiseId: item.VariantID,
				Quantity:      item.Quantity,
			})
			price, _ := strconv.ParseFloat(item.Subtotal, 64)
			unitPrice, _ := strconv.ParseFloat(item.UnitPrice, 64)
			total += price
			totalShipReady += price
			totalChargedNow += price

			title := item.Title

			orderItems = append(orderItems, OrderItem{
				ShopifyVariantID: item.VariantID,
				Type:             "ship_ready",
				Quantity:         item.Quantity,
				ItemStatus:       "pending_payment",
				FulfillmentStep:  1,
				FinalAmount:      &price,
				Title:            &title,
				UnitPrice:        &unitPrice,
				AmountCharged:    &price,
				ImageSrc:         item.ImageSrc,
			})
		}

		email := ""
		if req.GuestInfo != nil {
			email = req.GuestInfo.Email
		}

		cartInput := shopify.CartCreateInput{
			Lines: sfItems,
		}
		if email != "" {
			cartInput.BuyerIdentity = &shopify.CartBuyerIdentityInput{Email: email}
		}

		chkRes, chkErr := s.shopClient.CreateStorefrontCart(ctx, cartInput)
		if chkErr != nil {
			return nil, fmt.Errorf("failed to create cart: %w", chkErr)
		}
		checkoutURL = chkRes.CheckoutUrl
	}

	var hasPreOrder bool

	// 4. Process PreOrder (Admin Draft Order)
	if len(cartRes.PreOrder) > 0 {
		hasPreOrder = true
		var draftItems []shopify.DraftOrderLineItem
		for _, item := range cartRes.PreOrder {
			draftItems = append(draftItems, shopify.DraftOrderLineItem{
				VariantID: item.VariantID,
				Quantity:  item.Quantity,
			})
			dep, _ := strconv.ParseFloat(item.DepositAmount, 64)
			total += dep
			totalDepositPaid += dep
			totalChargedNow += dep

			bal, _ := strconv.ParseFloat(item.BalanceDue, 64)
			totalBalanceDue += bal

			unitPrice, _ := strconv.ParseFloat(item.UnitPrice, 64)
			title := item.Title

			orderItems = append(orderItems, OrderItem{
				ShopifyVariantID: item.VariantID,
				Type:             "pre_order",
				Quantity:         item.Quantity,
				ItemStatus:       "pending_deposit",
				FulfillmentStep:  1,
				DpAmount:         &dep,
				Title:            &title,
				UnitPrice:        &unitPrice,
				AmountCharged:    &dep,
				BalanceDue:       &bal,
				ImageSrc:         item.ImageSrc,
			})
		}

		draftInput := shopify.DraftOrderInput{
			LineItems: draftItems,
		}
		if req.GuestInfo != nil {
			draftInput.Email = req.GuestInfo.Email
		}
		if req.ShippingTitle != "" && req.ShippingPrice != "" {
			draftInput.ShippingLine = shopify.NewShippingLineInput(
				req.ShippingTitle,
				req.ShippingPrice,
				"USD",
			)
			total += shippingCost
			totalChargedNow += shippingCost
		}

		draftRes, draftErr := s.shopClient.CreateDraftOrder(ctx, draftInput)
		if draftErr != nil {
			return nil, fmt.Errorf("failed to create draft order: %w", draftErr)
		}
		draftInvoiceURL = draftRes.InvoiceUrl
	}

	// 5. Save Order
	orderNumber := fmt.Sprintf("ORD-%s", uuid.NewString()[:8])
	var shipMethodPtr *string
	if req.ShippingMethod != "" {
		shipMethodPtr = &req.ShippingMethod
	}

	order := &Order{
		OrderNumber:       orderNumber,
		CustomerID:        customerID,
		TotalPrice:        total,
		AggregateStatus:   "pending_payment",
		FinancialStatus:   "pending",
		FulfillmentStatus: "pending",
		TotalShipReady:    totalShipReady,
		TotalDepositPaid:  totalDepositPaid,
		TotalBalanceDue:   totalBalanceDue,
		TotalChargedNow:   totalChargedNow,
		ShippingMethod:    shipMethodPtr,
		ShippingCost:      shippingCost,
		Currency:          "USD",
		Items:             orderItems,
	}

	if err := s.store.CreateOrder(ctx, order); err != nil {
		return nil, apierror.ErrInternal
	}

	// Auto-create settlement for pre_order items
	if hasPreOrder {
		for _, item := range order.Items {
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

	// Clear cart after successful order creation
	_ = s.cartService.ClearCart(ctx, userID, sessionID)

	// Map to response
	var shipReady []OrderItemDetail
	var preOrder []OrderItemDetail

	for _, it := range order.Items {
		var itemTitle string
		if it.Title != nil {
			itemTitle = *it.Title
		}
		unitPrice := formatItemUnitPrice(it)
		var amountCharged, balanceDue *string
		if it.AmountCharged != nil {
			val := fmt.Sprintf("%.2f", *it.AmountCharged)
			amountCharged = &val
		}
		if it.BalanceDue != nil {
			val := fmt.Sprintf("%.2f", *it.BalanceDue)
			balanceDue = &val
		}

		detail := OrderItemDetail{
			ID:                it.ID,
			VariantID:         it.ShopifyVariantID,
			Type:              it.Type,
			Quantity:          it.Quantity,
			ItemStatus:        it.ItemStatus,
			FulfillmentStep:   it.FulfillmentStep,
			ItemsReceived:     effectiveItemsReceived(it),
			Title:             itemTitle,
			UnitPrice:         unitPrice,
			AmountCharged:     amountCharged,
			BalanceDue:        balanceDue,
			ImageSrc:          it.ImageSrc,
			TrackingNumber:    it.TrackingNumber,
			TrackingURL:       it.TrackingURL,
			TrackingCompany:   it.TrackingCompany,
			TrackingLastEvent: it.TrackingLastEvent,
		}
		if it.DpAmount != nil {
			val := fmt.Sprintf("%.2f", *it.DpAmount)
			detail.DpAmount = &val
		}
		if it.FinalAmount != nil {
			val := fmt.Sprintf("%.2f", *it.FinalAmount)
			detail.FinalAmount = &val
		}
		if it.Type == "ship_ready" {
			shipReady = append(shipReady, detail)
		} else {
			preOrder = append(preOrder, detail)
		}
	}

	if shipReady == nil {
		shipReady = []OrderItemDetail{}
	}
	if preOrder == nil {
		preOrder = []OrderItemDetail{}
	}

	response := &OrderResponse{
		ID:                  order.ID,
		OrderNumber:         order.OrderNumber,
		OrderDate:           order.CreatedAt.Format(time.RFC3339),
		ShopifyCheckoutURL:  checkoutURL,
		ShopifyDraftInvoice: draftInvoiceURL,
		TotalPrice:          fmt.Sprintf("%.2f", order.TotalPrice),
		AggregateStatus:     order.AggregateStatus,
		FinancialStatus:     order.FinancialStatus,
		FulfillmentStatus:   order.FulfillmentStatus,
		TotalShipReady:      fmt.Sprintf("%.2f", order.TotalShipReady),
		TotalDepositPaid:    fmt.Sprintf("%.2f", order.TotalDepositPaid),
		TotalBalanceDue:     fmt.Sprintf("%.2f", order.TotalBalanceDue),
		TotalChargedNow:     fmt.Sprintf("%.2f", order.TotalChargedNow),
		Currency:            order.Currency,
		LineItems:           LineItemsGroup{ShipReady: shipReady, PreOrder: preOrder},
	}

	if order.Customer != nil {
		firstName := ""
		lastName := ""
		if order.Customer.FirstName != nil {
			firstName = *order.Customer.FirstName
		}
		if order.Customer.LastName != nil {
			lastName = *order.Customer.LastName
		}
		phone := ""
		if order.Customer.Phone != nil {
			phone = *order.Customer.Phone
		}

		response.Customer = &CustomerDTO{
			ID:        order.Customer.ID,
			FirstName: firstName,
			LastName:  lastName,
			Email:     order.Customer.Email,
			Phone:     phone,
		}
	}

	response.ShippingAddress = addressToDTO(order.ShippingAddress)
	response.BillingAddress = addressToDTO(order.BillingAddress)

	// Shopify automatically sends order confirmations, so we skip sending our own here.

	return response, nil
}

func (s *service) GetOrders(ctx context.Context, userID string, query OrderQuery) ([]OrderResponse, int64, error) {
	orders, total, err := s.store.GetOrdersByCustomer(ctx, userID, query)
	if err != nil {
		return nil, 0, apierror.ErrInternal
	}

	var res []OrderResponse
	for _, o := range orders {
		var shipReady []OrderItemDetail
		var preOrder []OrderItemDetail
		for _, it := range o.Items {
			var itemTitle string
			if it.Title != nil {
				itemTitle = *it.Title
			}
			unitPrice := formatItemUnitPrice(it)
			var amountCharged, balanceDue *string
			if it.AmountCharged != nil {
				val := fmt.Sprintf("%.2f", *it.AmountCharged)
				amountCharged = &val
			}
			if it.BalanceDue != nil {
				val := fmt.Sprintf("%.2f", *it.BalanceDue)
				balanceDue = &val
			}

			detail := OrderItemDetail{
				ID:                it.ID,
				VariantID:         it.ShopifyVariantID,
				Type:              it.Type,
				Quantity:          it.Quantity,
				ItemStatus:        it.ItemStatus,
				FulfillmentStep:   it.FulfillmentStep,
				ItemsReceived:     effectiveItemsReceived(it),
				Title:             itemTitle,
				UnitPrice:         unitPrice,
				AmountCharged:     amountCharged,
				BalanceDue:        balanceDue,
				ImageSrc:          it.ImageSrc,
				TrackingNumber:    it.TrackingNumber,
				TrackingURL:       it.TrackingURL,
				TrackingCompany:   it.TrackingCompany,
				TrackingLastEvent: it.TrackingLastEvent,
			}
			if it.DpAmount != nil {
				val := fmt.Sprintf("%.2f", *it.DpAmount)
				detail.DpAmount = &val
			}
			if it.FinalAmount != nil {
				val := fmt.Sprintf("%.2f", *it.FinalAmount)
				detail.FinalAmount = &val
			}

			if it.Type == "ship_ready" {
				shipReady = append(shipReady, detail)
			} else {
				preOrder = append(preOrder, detail)
			}
		}
		if shipReady == nil {
			shipReady = []OrderItemDetail{}
		}
		if preOrder == nil {
			preOrder = []OrderItemDetail{}
		}

		dto := OrderResponse{
			ID:                o.ID,
			OrderNumber:       o.OrderNumber,
			OrderDate:         o.CreatedAt.Format(time.RFC3339),
			TotalPrice:        fmt.Sprintf("%.2f", o.TotalPrice),
			AggregateStatus:   o.AggregateStatus,
			FinancialStatus:   o.FinancialStatus,
			FulfillmentStatus: o.FulfillmentStatus,
			TotalShipReady:    fmt.Sprintf("%.2f", o.TotalShipReady),
			TotalDepositPaid:  fmt.Sprintf("%.2f", o.TotalDepositPaid),
			TotalBalanceDue:   fmt.Sprintf("%.2f", o.TotalBalanceDue),
			TotalChargedNow:   fmt.Sprintf("%.2f", o.TotalChargedNow),
			Currency:          o.Currency,
			LineItems:         LineItemsGroup{ShipReady: shipReady, PreOrder: preOrder},
		}

		if o.Customer != nil {
			firstName := ""
			lastName := ""
			if o.Customer.FirstName != nil {
				firstName = *o.Customer.FirstName
			}
			if o.Customer.LastName != nil {
				lastName = *o.Customer.LastName
			}
			phone := ""
			if o.Customer.Phone != nil {
				phone = *o.Customer.Phone
			}

			dto.Customer = &CustomerDTO{
				ID:        o.Customer.ID,
				FirstName: firstName,
				LastName:  lastName,
				Email:     o.Customer.Email,
				Phone:     phone,
			}
		}

		dto.ShippingAddress = addressToDTO(o.ShippingAddress)
		dto.BillingAddress = addressToDTO(o.BillingAddress)

		res = append(res, dto)
	}
	if res == nil {
		res = []OrderResponse{}
	}
	return res, total, nil
}

func (s *service) GetOrder(ctx context.Context, userID, orderID string) (*OrderResponse, error) {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}

	dims := s.enrichOrderItemDetails(ctx, o.Items)

	var shipReady []OrderItemDetail
	var preOrder []OrderItemDetail
	for _, it := range o.Items {
		var itemTitle string
		if it.Title != nil {
			itemTitle = *it.Title
		}
		unitPrice := formatItemUnitPrice(it)
		var amountCharged, balanceDue *string
		if it.AmountCharged != nil {
			val := fmt.Sprintf("%.2f", *it.AmountCharged)
			amountCharged = &val
		}
		if it.BalanceDue != nil {
			val := fmt.Sprintf("%.2f", *it.BalanceDue)
			balanceDue = &val
		}

		detail := OrderItemDetail{
			ID:                it.ID,
			VariantID:         it.ShopifyVariantID,
			Type:              it.Type,
			Quantity:          it.Quantity,
			ItemStatus:        it.ItemStatus,
			FulfillmentStep:   it.FulfillmentStep,
			ItemsReceived:     effectiveItemsReceived(it),
			Title:             itemTitle,
			UnitPrice:         unitPrice,
			AmountCharged:     amountCharged,
			BalanceDue:        balanceDue,
			ImageSrc:          it.ImageSrc,
			TrackingNumber:    it.TrackingNumber,
			TrackingURL:       it.TrackingURL,
			TrackingCompany:   it.TrackingCompany,
			TrackingLastEvent: it.TrackingLastEvent,
		}
		if it.DpAmount != nil {
			val := fmt.Sprintf("%.2f", *it.DpAmount)
			detail.DpAmount = &val
		}
		if it.FinalAmount != nil {
			val := fmt.Sprintf("%.2f", *it.FinalAmount)
			detail.FinalAmount = &val
		}

		if d, ok := dims[it.ShopifyVariantID]; ok {
			detail.SKU = d.SKU
			detail.WeightKg = d.WeightKg
			detail.WidthCm = d.WidthCm
			detail.HeightCm = d.HeightCm
			detail.DepthCm = d.DepthCm
		}

		if it.Type == "ship_ready" {
			shipReady = append(shipReady, detail)
		} else {
			preOrder = append(preOrder, detail)
		}
	}
	if shipReady == nil {
		shipReady = []OrderItemDetail{}
	}
	if preOrder == nil {
		preOrder = []OrderItemDetail{}
	}

	dto := &OrderResponse{
		ID:                  o.ID,
		OrderNumber:         o.OrderNumber,
		OrderDate:           o.CreatedAt.Format(time.RFC3339),
		ShopifyCheckoutURL:  "",
		ShopifyDraftInvoice: "",
		TotalPrice:          fmt.Sprintf("%.2f", o.TotalPrice),
		AggregateStatus:     o.AggregateStatus,
		FinancialStatus:     o.FinancialStatus,
		FulfillmentStatus:   o.FulfillmentStatus,
		TotalShipReady:      fmt.Sprintf("%.2f", o.TotalShipReady),
		TotalDepositPaid:    fmt.Sprintf("%.2f", o.TotalDepositPaid),
		TotalBalanceDue:     fmt.Sprintf("%.2f", o.TotalBalanceDue),
		TotalChargedNow:     fmt.Sprintf("%.2f", o.TotalChargedNow),
		Currency:            o.Currency,
		LineItems:           LineItemsGroup{ShipReady: shipReady, PreOrder: preOrder},
	}

	if o.ShippingMethod != nil {
		dto.ShippingMethod = *o.ShippingMethod
	}

	if len(preOrder) > 0 {
		shipment, err := s.store.GetPreorderShipment(ctx, orderID)
		if err != nil {
			return nil, apierror.ErrInternal
		}
		if shipment != nil {
			shipmentDTO := s.toPreorderShipmentDTO(shipment)
			dto.PreorderShipment = &shipmentDTO
		}
	}

	s.enrichRemainingQuantities(ctx, orderID, preOrder)
	dto.Fulfillments = s.loadFulfillmentDTOs(ctx, o)

	detailsByID := make(map[string]OrderItemDetail, len(shipReady)+len(preOrder))
	for _, d := range shipReady {
		detailsByID[d.ID] = d
	}
	for _, d := range preOrder {
		detailsByID[d.ID] = d
	}
	groups, secondPayment, err := s.buildFulfillmentGroups(ctx, o, detailsByID, dto.Fulfillments)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	dto.FulfillmentGroups = groups
	dto.SecondPayment = secondPayment

	if o.Customer != nil {
		firstName := ""
		lastName := ""
		if o.Customer.FirstName != nil {
			firstName = *o.Customer.FirstName
		}
		if o.Customer.LastName != nil {
			lastName = *o.Customer.LastName
		}
		phone := ""
		if o.Customer.Phone != nil {
			phone = *o.Customer.Phone
		}

		dto.Customer = &CustomerDTO{
			ID:        o.Customer.ID,
			FirstName: firstName,
			LastName:  lastName,
			Email:     o.Customer.Email,
			Phone:     phone,
		}
	}

	dto.ShippingAddress = addressToDTO(o.ShippingAddress)
	dto.BillingAddress = addressToDTO(o.BillingAddress)

	return dto, nil
}

func (s *service) GetOrderByShopifyID(ctx context.Context, shopifyOrderID string) (*OrderResponse, error) {
	o, err := s.store.GetOrderByShopifyID(ctx, shopifyOrderID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}
	// We can reuse the same response building logic
	// But GetOrderByShopifyID might not be tied to the specific request user (e.g. guest callback)
	// So we don't check userID here.

	var shipReady []OrderItemDetail
	var preOrder []OrderItemDetail
	for _, it := range o.Items {
		var itemTitle string
		if it.Title != nil {
			itemTitle = *it.Title
		}
		unitPrice := formatItemUnitPrice(it)
		var amountCharged, balanceDue *string
		if it.AmountCharged != nil {
			val := fmt.Sprintf("%.2f", *it.AmountCharged)
			amountCharged = &val
		}
		if it.BalanceDue != nil {
			val := fmt.Sprintf("%.2f", *it.BalanceDue)
			balanceDue = &val
		}

		detail := OrderItemDetail{
			ID:                it.ID,
			VariantID:         it.ShopifyVariantID,
			Type:              it.Type,
			Quantity:          it.Quantity,
			ItemStatus:        it.ItemStatus,
			FulfillmentStep:   it.FulfillmentStep,
			ItemsReceived:     effectiveItemsReceived(it),
			Title:             itemTitle,
			UnitPrice:         unitPrice,
			AmountCharged:     amountCharged,
			BalanceDue:        balanceDue,
			ImageSrc:          it.ImageSrc,
			TrackingNumber:    it.TrackingNumber,
			TrackingURL:       it.TrackingURL,
			TrackingCompany:   it.TrackingCompany,
			TrackingLastEvent: it.TrackingLastEvent,
		}
		if it.DpAmount != nil {
			val := fmt.Sprintf("%.2f", *it.DpAmount)
			detail.DpAmount = &val
		}
		if it.FinalAmount != nil {
			val := fmt.Sprintf("%.2f", *it.FinalAmount)
			detail.FinalAmount = &val
		}

		if it.Type == "ship_ready" {
			shipReady = append(shipReady, detail)
		} else {
			preOrder = append(preOrder, detail)
		}
	}
	if shipReady == nil {
		shipReady = []OrderItemDetail{}
	}
	if preOrder == nil {
		preOrder = []OrderItemDetail{}
	}

	dto := &OrderResponse{
		ID:                o.ID,
		OrderNumber:       o.OrderNumber,
		OrderDate:         o.CreatedAt.Format(time.RFC3339),
		TotalPrice:        fmt.Sprintf("%.2f", o.TotalPrice),
		AggregateStatus:   o.AggregateStatus,
		FinancialStatus:   o.FinancialStatus,
		FulfillmentStatus: o.FulfillmentStatus,
		TotalShipReady:    fmt.Sprintf("%.2f", o.TotalShipReady),
		TotalDepositPaid:  fmt.Sprintf("%.2f", o.TotalDepositPaid),
		TotalBalanceDue:   fmt.Sprintf("%.2f", o.TotalBalanceDue),
		TotalChargedNow:   fmt.Sprintf("%.2f", o.TotalChargedNow),
		Currency:          o.Currency,
		LineItems:         LineItemsGroup{ShipReady: shipReady, PreOrder: preOrder},
	}

	if o.Customer != nil {
		firstName := ""
		lastName := ""
		if o.Customer.FirstName != nil {
			firstName = *o.Customer.FirstName
		}
		if o.Customer.LastName != nil {
			lastName = *o.Customer.LastName
		}
		phone := ""
		if o.Customer.Phone != nil {
			phone = *o.Customer.Phone
		}

		dto.Customer = &CustomerDTO{
			ID:        o.Customer.ID,
			FirstName: firstName,
			LastName:  lastName,
			Email:     o.Customer.Email,
			Phone:     phone,
		}
	}

	dto.ShippingAddress = addressToDTO(o.ShippingAddress)
	dto.BillingAddress = addressToDTO(o.BillingAddress)

	if len(preOrder) > 0 {
		shipment, err := s.store.GetPreorderShipment(ctx, o.ID)
		if err != nil {
			return nil, apierror.ErrInternal
		}
		if shipment != nil {
			shipmentDTO := s.toPreorderShipmentDTO(shipment)
			dto.PreorderShipment = &shipmentDTO
		}
	}

	return dto, nil
}

func (s *service) AcceptOrder(ctx context.Context, userID, orderID, fulfillmentType string) error {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return apierror.ErrInternal
	}
	if o == nil {
		return apierror.ErrNotFound
	}

	// Orders created via webhook are set to "processing" (paid).
	// Orders created manually (local flow) are "pending_payment".
	validStatuses := map[string]bool{"pending": true, "pending_payment": true, "processing": true, "on_progress": true}
	if !validStatuses[o.AggregateStatus] {
		return apierror.New(400, "invalid_transition", fmt.Sprintf("Order cannot be accepted from status: %s", o.AggregateStatus))
	}

	// Update the overall order status
	if err := s.store.UpdateOrderStatus(ctx, orderID, "on_progress", o.FinancialStatus, "in_progress"); err != nil {
		return apierror.ErrInternal
	}

	// Update the item status for the specifically accepted type
	if err := s.store.UpdateItemStatusByType(ctx, orderID, fulfillmentType, "on_progress"); err != nil {
		return apierror.ErrInternal
	}

	// Advance ship-ready items only; pre-order stays at step 1 until shipping is configured.
	if fulfillmentType == "ship_ready" {
		if err := s.store.UpdateItemStepByType(ctx, orderID, fulfillmentType, 2); err != nil {
			return apierror.ErrInternal
		}
	}

	// NEW: If ship_ready, push to Shopify fulfillment dashboard
	if fulfillmentType == "ship_ready" {
		if o.ShopifyOrderID != nil && *o.ShopifyOrderID != "" {
			go func() {
				if err := s.shopClient.CreateFulfillment(context.Background(), *o.ShopifyOrderID); err != nil {
					slog.Error("failed to push order to shopify fulfillment", "order_id", orderID, "error", err)
				}
			}()
		} else {
			slog.Warn("Skipping Shopify fulfillment because ShopifyOrderID is empty or nil", "order_id", orderID)
		}
	}
	return nil
}

func (s *service) CancelOrder(ctx context.Context, userID, orderID, fulfillmentType, reason string) error {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return apierror.ErrInternal
	}
	if o == nil {
		return apierror.ErrNotFound
	}

	// Update the item status for the specifically cancelled type
	if err := s.store.UpdateItemStatusByType(ctx, orderID, fulfillmentType, "cancelled"); err != nil {
		return apierror.ErrInternal
	}

	if fulfillmentType == "pre_order" && s.batchService != nil {
		for _, item := range o.Items {
			if item.Type != "pre_order" {
				continue
			}
			if err := s.batchService.ReleaseAllocationsByOrderLineItemID(ctx, item.ID); err != nil {
				return err
			}
		}
	}

	// Check if ALL items are cancelled, if so, cancel the whole order
	allCancelled := true
	for _, it := range o.Items {
		if it.Type != fulfillmentType && it.ItemStatus != "cancelled" {
			allCancelled = false
			break
		}
	}

	if allCancelled {
		if err := s.store.UpdateOrderStatus(ctx, orderID, "cancelled", "refunded", "cancelled"); err != nil {
			return apierror.ErrInternal
		}
	}
	return nil
}

func (s *service) UpdateFulfillmentStep(ctx context.Context, userID, orderID, itemID string, step int) error {
	if step < 1 || step > 5 {
		return apierror.New(400, "invalid_step", "Step must be between 1 and 5")
	}
	if err := s.store.UpdateOrderItemStep(ctx, itemID, step); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func (s *service) UpdateItemsReceived(ctx context.Context, userID, orderID string, items []UpdateReceivedItem) error {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return apierror.ErrInternal
	}
	if o == nil {
		return apierror.ErrNotFound
	}

	for _, item := range items {
		if item.ItemsReceived < 0 {
			return apierror.New(400, "invalid_count", "Count cannot be negative")
		}

		deliveredStep := 4
		for _, dbItem := range o.Items {
			if dbItem.ID == item.ItemID {
				if dbItem.Type == "pre_order" {
					deliveredStep = preOrderStepDelivered
				}
				break
			}
		}

		if err := s.store.UpdateOrderItemReceived(ctx, item.ItemID, item.ItemsReceived, deliveredStep); err != nil {
			slog.WarnContext(ctx, "failed to update item received", slog.String("item_id", item.ItemID), slog.Any("error", err))
		}

		// Update the item status in our memory object so we can check it below
		for i, dbItem := range o.Items {
			if dbItem.ID == item.ItemID {
				o.Items[i].ItemStatus = "delivered"
			}
		}
	}

	// Check if ALL items in the order are now delivered
	allDelivered := true
	for _, it := range o.Items {
		if it.ItemStatus != "delivered" {
			allDelivered = false
			break
		}
	}

	if allDelivered {
		// aggregateStatus = "completed", financialStatus = o.FinancialStatus, fulfillmentStatus = "fulfilled"
		if err := s.store.UpdateOrderStatus(ctx, orderID, "completed", o.FinancialStatus, "fulfilled"); err != nil {
			slog.ErrorContext(ctx, "failed to update master order status to completed", slog.String("order_id", orderID), slog.Any("error", err))
		}
	}

	return nil
}

func (s *service) AddTrackingNumber(ctx context.Context, userID, orderID string, itemIDs []string, trackingNumber, trackingURL string) error {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return apierror.ErrInternal
	}
	if o == nil {
		return apierror.ErrNotFound
	}

	now := time.Now()
	// When adding tracking manually via admin panel, we don't know company/event yet.
	for _, itemID := range itemIDs {
		fulfillmentStep := 3
		for _, it := range o.Items {
			if it.ID == itemID {
				if it.Type == "pre_order" {
					fulfillmentStep = preOrderStepShipped
				}
				break
			}
		}
		if err := s.store.UpdateOrderItemTracking(ctx, itemID, trackingNumber, trackingURL, "", "", "shipped", fulfillmentStep, &now); err != nil {
			slog.WarnContext(ctx, "failed to add tracking to item", slog.String("item_id", itemID), slog.Any("error", err))
		}
	}

	// Trigger email in goroutine
	go func() {
		bgCtx := context.Background()
		user, _ := s.authStore.GetUserByID(bgCtx, o.CustomerID)
		if user != nil {
			emailData := email.ShipmentEmailData{
				CustomerName:   "Customer", // Could be from User struct if added
				OrderNumber:    o.OrderNumber,
				Carrier:        "Standard Shipping",
				TrackingNumber: trackingNumber,
				TrackingURL:    trackingURL,
			}
			_ = s.emailService.SendShipmentDispatched(bgCtx, user.Email, emailData)
		}
	}()

	return nil
}

func (s *service) ExportOrdersToExcel(ctx context.Context, query OrderQuery) ([]byte, error) {
	orders, err := s.store.GetAllOrdersForExport(ctx, query)
	if err != nil {
		return nil, err
	}

	f := excelize.NewFile()
	sheetName := "Sales Report"
	f.SetSheetName("Sheet1", sheetName)

	headers := []string{"Order ID", "Date", "Customer Name", "Customer Email", "Ship Ready Items", "Pre-Order Items", "Total Price", "Financial Status", "Fulfillment Status"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, h)
	}

	for rowIdx, o := range orders {
		shipReadyQty := 0
		preOrderQty := 0
		for _, item := range o.Items {
			if item.Type == "ship_ready" {
				shipReadyQty += item.Quantity
			} else if item.Type == "pre_order" {
				preOrderQty += item.Quantity
			}
		}

		rowNum := rowIdx + 2
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", rowNum), o.OrderNumber)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", rowNum), o.CreatedAt.Format("2006-01-02"))
		if o.Customer != nil {
			var fname, lname string
			if o.Customer.FirstName != nil {
				fname = *o.Customer.FirstName
			}
			if o.Customer.LastName != nil {
				lname = *o.Customer.LastName
			}
			f.SetCellValue(sheetName, fmt.Sprintf("C%d", rowNum), fname+" "+lname)
			f.SetCellValue(sheetName, fmt.Sprintf("D%d", rowNum), o.Customer.Email)
		}
		f.SetCellValue(sheetName, fmt.Sprintf("E%d", rowNum), shipReadyQty)
		f.SetCellValue(sheetName, fmt.Sprintf("F%d", rowNum), preOrderQty)
		f.SetCellValue(sheetName, fmt.Sprintf("G%d", rowNum), fmt.Sprintf("%.2f", o.TotalPrice))
		f.SetCellValue(sheetName, fmt.Sprintf("H%d", rowNum), o.FinancialStatus)
		f.SetCellValue(sheetName, fmt.Sprintf("I%d", rowNum), o.FulfillmentStatus)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write excel file: %w", err)
	}
	return buf.Bytes(), nil
}

func (s *service) GetOrderTracking(ctx context.Context, userID, orderID string) ([]shipstation.TrackingResponse, error) {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}

	var results []shipstation.TrackingResponse
	seenTracking := make(map[string]bool)

	for _, targetItem := range o.Items {
		if targetItem.TrackingNumber == nil || *targetItem.TrackingNumber == "" {
			continue
		}

		trackingNum := *targetItem.TrackingNumber
		if seenTracking[trackingNum] {
			continue // We already fetched tracking for this package
		}
		seenTracking[trackingNum] = true

		var currentRes *shipstation.TrackingResponse

		// For pre_order, fetch live tracking via ShipStation
		if targetItem.Type == "pre_order" {
			carrierCode := ""
			if targetItem.TrackingCompany != nil {
				carrierCode = strings.ToLower(*targetItem.TrackingCompany)
			}

			if carrierCode != "" {
				res, err := s.shipstationClient.TrackShipment(ctx, carrierCode, trackingNum)
				if err == nil && res != nil {
					currentRes = res
				}
			}
		}

		// Fallback to DB record if ShipStation failed or it's ship_ready
		if currentRes == nil {
			currentRes = &shipstation.TrackingResponse{
				TrackingNumber: trackingNum,
			}
			if targetItem.TrackingLastEvent != nil {
				currentRes.StatusDescription = *targetItem.TrackingLastEvent
			} else {
				currentRes.StatusDescription = "Package departed from facility"
			}

			if targetItem.TrackingCompany != nil {
				currentRes.CarrierCode = *targetItem.TrackingCompany
			}

			if targetItem.ShippedAt != nil {
				currentRes.ShipDate = targetItem.ShippedAt.Format(time.RFC3339)
			}
		}

		results = append(results, *currentRes)
	}

	return results, nil
}

// effectiveItemsReceived returns items_received for API responses. Delivered items
// that predate the items_received write path still show as fully received.
func effectiveItemsReceived(it OrderItem) int {
	if it.ItemsReceived > 0 {
		if it.Quantity > 0 && it.ItemsReceived > it.Quantity {
			return it.Quantity
		}
		return it.ItemsReceived
	}
	delivered := it.ItemStatus == "delivered"
	if !delivered {
		if it.Type == "pre_order" {
			delivered = it.FulfillmentStep >= preOrderStepDelivered
		} else {
			delivered = it.FulfillmentStep >= 4
		}
	}
	if delivered && it.Quantity > 0 {
		return it.Quantity
	}
	return 0
}
