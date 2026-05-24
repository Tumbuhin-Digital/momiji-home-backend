package order

import (
	"context"
	"fmt"
	"strconv"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/auth"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"golang.org/x/crypto/bcrypt"
	"github.com/google/uuid"
)

type OrderService interface {
	CreateOrder(ctx context.Context, userID, sessionID *string, req CreateOrderRequest) (*OrderResponse, error)
	GetOrders(ctx context.Context, userID string) ([]OrderResponse, error)
}

type service struct {
	store         Store
	cartService   cart.CartService
	authStore     auth.AuthStore
	shopClient    shopify.Client
	preorderStore preorder.PreorderStore
}

func NewOrderService(store Store, cartService cart.CartService, authStore auth.AuthStore, shopClient shopify.Client, preorderStore preorder.PreorderStore) OrderService {
	return &service{
		store:         store,
		cartService:   cartService,
		authStore:     authStore,
		shopClient:    shopClient,
		preorderStore: preorderStore,
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

	// 3. Process ShipReady (Storefront Checkout)
	if len(cartRes.ShipReady) > 0 {
		var sfItems []shopify.CheckoutLineItem
		for _, item := range cartRes.ShipReady {
			sfItems = append(sfItems, shopify.CheckoutLineItem{
				VariantID: item.VariantID,
				Quantity:  item.Quantity,
			})
			price, _ := strconv.ParseFloat(item.Subtotal, 64)
			total += price
			
			orderItems = append(orderItems, OrderItem{
				ShopifyVariantID: item.VariantID,
				Type:             "ship_ready",
				Quantity:         item.Quantity,
				ItemStatus:       "pending_payment",
				FinalAmount:      &price,
			})
		}

		email := ""
		if req.GuestInfo != nil {
			email = req.GuestInfo.Email
		}
		checkoutInput := shopify.CheckoutCreateInput{
			Email:     email,
			LineItems: sfItems,
		}
		
		chkRes, chkErr := s.shopClient.CreateStorefrontCheckout(ctx, checkoutInput)
		if chkErr != nil {
			return nil, fmt.Errorf("failed to create checkout: %w", chkErr)
		}
		checkoutURL = chkRes.WebUrl
	}

	var hasPreOrder bool
	var settlementAmount float64

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
			
			bal, _ := strconv.ParseFloat(item.BalanceDue, 64)
			settlementAmount += bal
			
			orderItems = append(orderItems, OrderItem{
				ShopifyVariantID: item.VariantID,
				Type:             "pre_order",
				Quantity:         item.Quantity,
				ItemStatus:       "pending_deposit",
				DpAmount:         &dep,
			})
		}
		
		draftInput := shopify.DraftOrderInput{
			LineItems: draftItems,
		}
		if req.GuestInfo != nil {
			draftInput.Customer = &shopify.DraftOrderCustomer{Email: req.GuestInfo.Email}
		}

		draftRes, draftErr := s.shopClient.CreateDraftOrder(ctx, draftInput)
		if draftErr != nil {
			return nil, fmt.Errorf("failed to create draft order: %w", draftErr)
		}
		draftInvoiceURL = draftRes.InvoiceUrl
	}

	// 5. Save Order
	order := &Order{
		CustomerID:      customerID,
		TotalPrice:      total,
		AggregateStatus: "pending_payment",
		Items:           orderItems,
	}

	if err := s.store.CreateOrder(ctx, order); err != nil {
		return nil, apierror.ErrInternal
	}

	// Auto-create settlement for pre_order items
	if hasPreOrder {
		settlement := &preorder.Settlement{
			OrderID: order.ID,
			Amount:  settlementAmount,
			Status:  "pending",
		}
		_ = s.preorderStore.CreateSettlement(ctx, settlement)
	}

	// Clear cart after successful order creation
	_ = s.cartService.ClearCart(ctx, userID, sessionID)

	// Map to response
	resItems := make([]OrderItemDetail, len(order.Items))
	for i, it := range order.Items {
		resItems[i] = OrderItemDetail{
			ID:         it.ID,
			VariantID:  it.ShopifyVariantID,
			Type:       it.Type,
			Quantity:   it.Quantity,
			ItemStatus: it.ItemStatus,
		}
		if it.DpAmount != nil {
			val := fmt.Sprintf("%.2f", *it.DpAmount)
			resItems[i].DpAmount = &val
		}
		if it.FinalAmount != nil {
			val := fmt.Sprintf("%.2f", *it.FinalAmount)
			resItems[i].FinalAmount = &val
		}
	}

	return &OrderResponse{
		ID:                  order.ID,
		ShopifyCheckoutURL:  checkoutURL,
		ShopifyDraftInvoice: draftInvoiceURL,
		TotalPrice:          fmt.Sprintf("%.2f", order.TotalPrice),
		AggregateStatus:     order.AggregateStatus,
		Items:               resItems,
	}, nil
}

func (s *service) GetOrders(ctx context.Context, userID string) ([]OrderResponse, error) {
	orders, err := s.store.GetOrdersByCustomer(ctx, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	
	var res []OrderResponse
	for _, o := range orders {
		resItems := make([]OrderItemDetail, len(o.Items))
		for i, it := range o.Items {
			resItems[i] = OrderItemDetail{
				ID:         it.ID,
				VariantID:  it.ShopifyVariantID,
				Type:       it.Type,
				Quantity:   it.Quantity,
				ItemStatus: it.ItemStatus,
			}
		}
		res = append(res, OrderResponse{
			ID:              o.ID,
			TotalPrice:      fmt.Sprintf("%.2f", o.TotalPrice),
			AggregateStatus: o.AggregateStatus,
			Items:           resItems,
		})
	}
	return res, nil
}
