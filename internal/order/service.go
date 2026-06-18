package order

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
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
	UpdateItemsReceived(ctx context.Context, userID, orderID, itemID string, count int) error
	AddTrackingNumber(ctx context.Context, userID, orderID, itemID, trackingNumber, trackingURL string) error
	GetItemTracking(ctx context.Context, userID, orderID, itemID string) (*shipstation.TrackingResponse, error)
	ExportOrdersToExcel(ctx context.Context, query OrderQuery) ([]byte, error)
}

type service struct {
	store             Store
	cartService       cart.CartService
	authStore         auth.AuthStore
	shopClient        shopify.Client
	shipstationClient shipstation.Client
	preorderStore     preorder.PreorderStore
	emailService      email.NotificationService
}

func NewOrderService(store Store, cartService cart.CartService, authStore auth.AuthStore, shopClient shopify.Client,
	preorderStore preorder.PreorderStore,
	notificationService email.NotificationService,
	shipstationClient shipstation.Client,
) OrderService {
	return &service{
		store:             store,
		cartService:       cartService,
		authStore:         authStore,
		shopClient:        shopClient,
		shipstationClient: shipstationClient,
		preorderStore:     preorderStore,
		emailService:      notificationService,
	}
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
			draftInput.ShippingLine = &shopify.ShippingLineInput{
				Title: req.ShippingTitle,
				Price: req.ShippingPrice,
			}
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
		var unitPrice, amountCharged, balanceDue *string
		if it.UnitPrice != nil {
			val := fmt.Sprintf("%.2f", *it.UnitPrice)
			unitPrice = &val
		}
		if it.AmountCharged != nil {
			val := fmt.Sprintf("%.2f", *it.AmountCharged)
			amountCharged = &val
		}
		if it.BalanceDue != nil {
			val := fmt.Sprintf("%.2f", *it.BalanceDue)
			balanceDue = &val
		}

		detail := OrderItemDetail{
			ID:              it.ID,
			VariantID:       it.ShopifyVariantID,
			Type:            it.Type,
			Quantity:        it.Quantity,
			ItemStatus:      it.ItemStatus,
			FulfillmentStep: it.FulfillmentStep,
			ItemsReceived:   it.ItemsReceived,
			Title:           itemTitle,
			UnitPrice:       unitPrice,
			AmountCharged:   amountCharged,
			BalanceDue:      balanceDue,
			ImageSrc:        it.ImageSrc,
			TrackingNumber:  it.TrackingNumber,
			TrackingURL:     it.TrackingURL,
			TrackingCompany: it.TrackingCompany,
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
		if order.Customer.FirstName != nil { firstName = *order.Customer.FirstName }
		if order.Customer.LastName != nil { lastName = *order.Customer.LastName }
		phone := ""
		if order.Customer.Phone != nil { phone = *order.Customer.Phone }

		response.Customer = &CustomerDTO{
			ID:        order.Customer.ID,
			FirstName: firstName,
			LastName:  lastName,
			Email:     order.Customer.Email,
			Phone:     phone,
		}
	}

	if order.ShippingAddress != nil {
		firstName := ""
		lastName := ""
		if order.ShippingAddress.FirstName != nil { firstName = *order.ShippingAddress.FirstName }
		if order.ShippingAddress.LastName != nil { lastName = *order.ShippingAddress.LastName }
		address2 := ""
		if order.ShippingAddress.Address2 != nil { address2 = *order.ShippingAddress.Address2 }
		phone := ""
		if order.ShippingAddress.Phone != nil { phone = *order.ShippingAddress.Phone }

		response.ShippingAddress = &AddressDTO{
			FirstName: firstName,
			LastName:  lastName,
			Address1:  order.ShippingAddress.Address1,
			Address2:  address2,
			City:      order.ShippingAddress.City,
			Province:  order.ShippingAddress.Province,
			Country:   order.ShippingAddress.Country,
			Zip:       order.ShippingAddress.Zip,
			Phone:     phone,
		}
	}

	// Trigger email notification
	go func() {
		// Use a background context so it isn't cancelled when the request ends
		bgCtx := context.Background()
		var customerName string
		customerEmail := ""
		if req.GuestInfo != nil {
			customerName = req.GuestInfo.FirstName
			customerEmail = req.GuestInfo.Email
		} else {
			// Find user details if needed, or simply pass email if available.
			user, _ := s.authStore.GetUserByID(bgCtx, customerID)
			if user != nil {
				customerEmail = user.Email
				customerName = "Customer"
			}
		}

		if customerEmail != "" {
			var emailItems []email.OrderItemData
			for _, it := range order.Items {
				title := ""
				if it.Title != nil {
					title = *it.Title
				}
				amt := 0.0
				if it.AmountCharged != nil {
					amt = *it.AmountCharged
				}
				emailItems = append(emailItems, email.OrderItemData{
					Title:    title,
					Type:     it.Type,
					Quantity: it.Quantity,
					Amount:   fmt.Sprintf("$%.2f", amt),
				})
			}
			emailData := email.OrderEmailData{
				CustomerName:    customerName,
				OrderNumber:     order.OrderNumber,
				Items:           emailItems,
				TotalPaid:       fmt.Sprintf("$%.2f", order.TotalChargedNow),
				HasBalanceDue:   hasPreOrder,
				TotalBalanceDue: fmt.Sprintf("$%.2f", order.TotalBalanceDue),
			}
			if err := s.emailService.SendOrderConfirmation(bgCtx, customerEmail, emailData); err != nil {
				slog.Error("Failed to send order confirmation email", slog.Any("error", err), slog.String("to", customerEmail))
			}
		}
	}()

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
			var unitPrice, amountCharged, balanceDue *string
			if it.UnitPrice != nil {
				val := fmt.Sprintf("%.2f", *it.UnitPrice)
				unitPrice = &val
			}
			if it.AmountCharged != nil {
				val := fmt.Sprintf("%.2f", *it.AmountCharged)
				amountCharged = &val
			}
			if it.BalanceDue != nil {
				val := fmt.Sprintf("%.2f", *it.BalanceDue)
				balanceDue = &val
			}

			detail := OrderItemDetail{
				ID:              it.ID,
				VariantID:       it.ShopifyVariantID,
				Type:            it.Type,
				Quantity:        it.Quantity,
				ItemStatus:      it.ItemStatus,
				FulfillmentStep: it.FulfillmentStep,
				ItemsReceived:   it.ItemsReceived,
				Title:           itemTitle,
				UnitPrice:       unitPrice,
				AmountCharged:   amountCharged,
				BalanceDue:      balanceDue,
				ImageSrc:        it.ImageSrc,
				TrackingNumber:  it.TrackingNumber,
				TrackingURL:     it.TrackingURL,
				TrackingCompany: it.TrackingCompany,
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
			if o.Customer.FirstName != nil { firstName = *o.Customer.FirstName }
			if o.Customer.LastName != nil { lastName = *o.Customer.LastName }
			phone := ""
			if o.Customer.Phone != nil { phone = *o.Customer.Phone }

			dto.Customer = &CustomerDTO{
				ID:        o.Customer.ID,
				FirstName: firstName,
				LastName:  lastName,
				Email:     o.Customer.Email,
				Phone:     phone,
			}
		}

		if o.ShippingAddress != nil {
			firstName := ""
			lastName := ""
			if o.ShippingAddress.FirstName != nil { firstName = *o.ShippingAddress.FirstName }
			if o.ShippingAddress.LastName != nil { lastName = *o.ShippingAddress.LastName }
			address2 := ""
			if o.ShippingAddress.Address2 != nil { address2 = *o.ShippingAddress.Address2 }
			phone := ""
			if o.ShippingAddress.Phone != nil { phone = *o.ShippingAddress.Phone }

			dto.ShippingAddress = &AddressDTO{
				FirstName: firstName,
				LastName:  lastName,
				Address1:  o.ShippingAddress.Address1,
				Address2:  address2,
				City:      o.ShippingAddress.City,
				Province:  o.ShippingAddress.Province,
				Country:   o.ShippingAddress.Country,
				Zip:       o.ShippingAddress.Zip,
				Phone:     phone,
			}
		}

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

	var shipReady []OrderItemDetail
	var preOrder []OrderItemDetail
	for _, it := range o.Items {
		var itemTitle string
		if it.Title != nil {
			itemTitle = *it.Title
		}
		var unitPrice, amountCharged, balanceDue *string
		if it.UnitPrice != nil {
			val := fmt.Sprintf("%.2f", *it.UnitPrice)
			unitPrice = &val
		}
		if it.AmountCharged != nil {
			val := fmt.Sprintf("%.2f", *it.AmountCharged)
			amountCharged = &val
		}
		if it.BalanceDue != nil {
			val := fmt.Sprintf("%.2f", *it.BalanceDue)
			balanceDue = &val
		}

		detail := OrderItemDetail{
			ID:              it.ID,
			VariantID:       it.ShopifyVariantID,
			Type:            it.Type,
			Quantity:        it.Quantity,
			ItemStatus:      it.ItemStatus,
			FulfillmentStep: it.FulfillmentStep,
			ItemsReceived:   it.ItemsReceived,
			Title:           itemTitle,
			UnitPrice:       unitPrice,
			AmountCharged:   amountCharged,
			BalanceDue:      balanceDue,
			ImageSrc:        it.ImageSrc,
			TrackingNumber:  it.TrackingNumber,
			TrackingURL:     it.TrackingURL,
			TrackingCompany: it.TrackingCompany,
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

	if o.Customer != nil {
		firstName := ""
		lastName := ""
		if o.Customer.FirstName != nil { firstName = *o.Customer.FirstName }
		if o.Customer.LastName != nil { lastName = *o.Customer.LastName }
		phone := ""
		if o.Customer.Phone != nil { phone = *o.Customer.Phone }

		dto.Customer = &CustomerDTO{
			ID:        o.Customer.ID,
			FirstName: firstName,
			LastName:  lastName,
			Email:     o.Customer.Email,
			Phone:     phone,
		}
	}

	if o.ShippingAddress != nil {
		firstName := ""
		lastName := ""
		if o.ShippingAddress.FirstName != nil { firstName = *o.ShippingAddress.FirstName }
		if o.ShippingAddress.LastName != nil { lastName = *o.ShippingAddress.LastName }
		address2 := ""
		if o.ShippingAddress.Address2 != nil { address2 = *o.ShippingAddress.Address2 }
		phone := ""
		if o.ShippingAddress.Phone != nil { phone = *o.ShippingAddress.Phone }

		dto.ShippingAddress = &AddressDTO{
			FirstName: firstName,
			LastName:  lastName,
			Address1:  o.ShippingAddress.Address1,
			Address2:  address2,
			City:      o.ShippingAddress.City,
			Province:  o.ShippingAddress.Province,
			Country:   o.ShippingAddress.Country,
			Zip:       o.ShippingAddress.Zip,
			Phone:     phone,
		}
	}

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
		var unitPrice, amountCharged, balanceDue *string
		if it.UnitPrice != nil {
			val := fmt.Sprintf("%.2f", *it.UnitPrice)
			unitPrice = &val
		}
		if it.AmountCharged != nil {
			val := fmt.Sprintf("%.2f", *it.AmountCharged)
			amountCharged = &val
		}
		if it.BalanceDue != nil {
			val := fmt.Sprintf("%.2f", *it.BalanceDue)
			balanceDue = &val
		}

		detail := OrderItemDetail{
			ID:              it.ID,
			VariantID:       it.ShopifyVariantID,
			Type:            it.Type,
			Quantity:        it.Quantity,
			ItemStatus:      it.ItemStatus,
			FulfillmentStep: it.FulfillmentStep,
			ItemsReceived:   it.ItemsReceived,
			Title:           itemTitle,
			UnitPrice:       unitPrice,
			AmountCharged:   amountCharged,
			BalanceDue:      balanceDue,
			ImageSrc:        it.ImageSrc,
			TrackingNumber:  it.TrackingNumber,
			TrackingURL:     it.TrackingURL,
			TrackingCompany: it.TrackingCompany,
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
		if o.Customer.FirstName != nil { firstName = *o.Customer.FirstName }
		if o.Customer.LastName != nil { lastName = *o.Customer.LastName }
		phone := ""
		if o.Customer.Phone != nil { phone = *o.Customer.Phone }

		dto.Customer = &CustomerDTO{
			ID:        o.Customer.ID,
			FirstName: firstName,
			LastName:  lastName,
			Email:     o.Customer.Email,
			Phone:     phone,
		}
	}

	if o.ShippingAddress != nil {
		firstName := ""
		lastName := ""
		if o.ShippingAddress.FirstName != nil { firstName = *o.ShippingAddress.FirstName }
		if o.ShippingAddress.LastName != nil { lastName = *o.ShippingAddress.LastName }
		address2 := ""
		if o.ShippingAddress.Address2 != nil { address2 = *o.ShippingAddress.Address2 }
		phone := ""
		if o.ShippingAddress.Phone != nil { phone = *o.ShippingAddress.Phone }

		dto.ShippingAddress = &AddressDTO{
			FirstName: firstName,
			LastName:  lastName,
			Address1:  o.ShippingAddress.Address1,
			Address2:  address2,
			City:      o.ShippingAddress.City,
			Province:  o.ShippingAddress.Province,
			Country:   o.ShippingAddress.Country,
			Zip:       o.ShippingAddress.Zip,
			Phone:     phone,
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
	validStatuses := map[string]bool{"pending": true, "pending_payment": true, "processing": true}
	if !validStatuses[o.AggregateStatus] {
		return apierror.New(400, "invalid_transition", fmt.Sprintf("Order cannot be accepted from status: %s", o.AggregateStatus))
	}

	// Update order status logic - simplified
	if err := s.store.UpdateOrderStatus(ctx, orderID, "on_progress", "in_progress"); err != nil {
		return apierror.ErrInternal
	}

	// NEW: If ship_ready, push to Shopify fulfillment dashboard
	if fulfillmentType == "ship_ready" && o.ShopifyOrderID != nil && *o.ShopifyOrderID != "" {
		go func() {
			if err := s.shopClient.CreateFulfillment(context.Background(), *o.ShopifyOrderID); err != nil {
				slog.Error("failed to push order to shopify fulfillment", "order_id", orderID, "error", err)
			}
		}()
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

	// TODO: log refund note
	if err := s.store.UpdateOrderStatus(ctx, orderID, "refunded", "cancelled"); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func (s *service) UpdateFulfillmentStep(ctx context.Context, userID, orderID, itemID string, step int) error {
	if step < 1 || step > 4 {
		return apierror.New(400, "invalid_step", "Step must be between 1 and 4")
	}
	if err := s.store.UpdateOrderItemStep(ctx, itemID, step); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func (s *service) UpdateItemsReceived(ctx context.Context, userID, orderID, itemID string, count int) error {
	if count < 0 {
		return apierror.New(400, "invalid_count", "Count cannot be negative")
	}
	if err := s.store.UpdateOrderItemReceived(ctx, itemID, count); err != nil {
		return apierror.ErrInternal
	}
	return nil
}

func (s *service) AddTrackingNumber(ctx context.Context, userID, orderID, itemID, trackingNumber, trackingURL string) error {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return apierror.ErrInternal
	}
	if o == nil {
		return apierror.ErrNotFound
	}

	now := time.Now()
	// When adding tracking manually via admin panel, we don't know company/event yet.
	if err := s.store.UpdateOrderItemTracking(ctx, itemID, trackingNumber, trackingURL, "", "", &now); err != nil {
		return apierror.ErrInternal
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
			if o.Customer.FirstName != nil { fname = *o.Customer.FirstName }
			if o.Customer.LastName != nil { lname = *o.Customer.LastName }
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

func (s *service) GetItemTracking(ctx context.Context, userID, orderID, itemID string) (*shipstation.TrackingResponse, error) {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}

	var targetItem *OrderItem
	for _, it := range o.Items {
		if it.ID == itemID {
			targetItem = &it
			break
		}
	}

	if targetItem == nil {
		return nil, apierror.ErrNotFound
	}

	if targetItem.TrackingNumber == nil || *targetItem.TrackingNumber == "" {
		return nil, apierror.ErrNotFound
	}

	// For pre_order, fetch live tracking via ShipStation
	if targetItem.Type == "pre_order" {
		carrierCode := ""
		if targetItem.TrackingCompany != nil {
			carrierCode = strings.ToLower(*targetItem.TrackingCompany)
		}
		
		if carrierCode != "" {
			res, err := s.shipstationClient.TrackShipment(ctx, carrierCode, *targetItem.TrackingNumber)
			if err == nil {
				return res, nil
			}
			// If error, fall through and return DB record
		}
	}

	// For ship_ready or if ShipStation fails, return the DB record
	res := &shipstation.TrackingResponse{
		TrackingNumber: *targetItem.TrackingNumber,
	}
	if targetItem.TrackingLastEvent != nil {
		res.StatusDescription = *targetItem.TrackingLastEvent
	} else {
		res.StatusDescription = "Package departed from facility"
	}

	if targetItem.TrackingCompany != nil {
		res.CarrierCode = *targetItem.TrackingCompany
	}
	
	if targetItem.ShippedAt != nil {
		res.ShipDate = targetItem.ShippedAt.Format(time.RFC3339)
	}

	return res, nil
}
