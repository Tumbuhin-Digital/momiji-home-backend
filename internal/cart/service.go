package cart

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type CartService interface {
	CreateGuestSession(ctx context.Context) (*GuestSessionResponse, error)
	GetCartResponse(ctx context.Context, userID, sessionID *string) (*CartResponse, error)
	GetCartSummary(ctx context.Context, userID, sessionID *string) (*CartSummaryDTO, error)
	AddItem(ctx context.Context, userID, sessionID *string, req CartItemRequest) error
	UpdateItemQuantity(ctx context.Context, userID, sessionID *string, itemID string, req UpdateCartItemRequest) error
	RemoveItem(ctx context.Context, userID, sessionID *string, itemID string) error
	ClearCart(ctx context.Context, userID, sessionID *string) error
	MergeCarts(ctx context.Context, userID string, guestSessionID string) error
}

type service struct {
	store          CartStore
	productService product.ProductService
}

func NewCartService(store CartStore, productService product.ProductService) CartService {
	return &service{store: store, productService: productService}
}

func (s *service) CreateGuestSession(ctx context.Context) (*GuestSessionResponse, error) {
	sessionID := "sess_" + uuid.NewString()
	expires := time.Now().Add(30 * 24 * time.Hour)
	
	cart := &Cart{
		SessionID: &sessionID,
		ExpiresAt: expires,
		Status:    "active",
	}

	if err := s.store.CreateCart(ctx, cart); err != nil {
		return nil, apierror.ErrInternal
	}

	return &GuestSessionResponse{
		SessionID: sessionID,
		ExpiresAt: expires.Format(time.RFC3339),
	}, nil
}

func (s *service) getOrCreateCart(ctx context.Context, userID, sessionID *string) (*Cart, error) {
	cart, err := s.store.GetCart(ctx, userID, sessionID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	
	if cart == nil {
		newCart := &Cart{
			UserID:    userID,
			SessionID: sessionID,
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
			Status:    "active",
		}
		if err := s.store.CreateCart(ctx, newCart); err != nil {
			return nil, apierror.ErrInternal
		}
		return newCart, nil
	}
	return cart, nil
}

func (s *service) GetCartResponse(ctx context.Context, userID, sessionID *string) (*CartResponse, error) {
	cart, err := s.store.GetCart(ctx, userID, sessionID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	res := &CartResponse{
		ShipReady: make([]CartItem, 0),
		PreOrder:  make([]CartItem, 0),
	}

	if cart != nil && cart.SessionID != nil {
		res.SessionID = *cart.SessionID
	}

	if cart == nil || len(cart.Items) == 0 {
		return res, nil
	}

	var totalShipReady, totalPreOrder, totalDeposit, totalBalanceDue, totalChargedNow float64

	for _, item := range cart.Items {
		variant, err := s.productService.GetVariantByID(ctx, item.ShopifyVariantID)
		if err != nil {
			continue // skip invalid items
		}

		subtotal := item.UnitPrice * float64(item.Quantity)
		
		cItem := CartItem{
			ID:        item.ID,
			VariantID: item.ShopifyVariantID,
			Title:     variant.Title,
			ImageSrc:  variant.ImageSrc,
			Quantity:  item.Quantity,
			UnitPrice: fmt.Sprintf("%.2f", item.UnitPrice),
			Subtotal:  fmt.Sprintf("%.2f", subtotal),
		}

		if item.FulfillmentType == string(product.FulfillmentTypePreOrder) {
			deposit := subtotal * 0.5 // 50% deposit rule
			balance := subtotal - deposit

			cItem.DepositAmount = fmt.Sprintf("%.2f", deposit)
			cItem.BalanceDue = fmt.Sprintf("%.2f", balance)

			totalPreOrder += subtotal
			totalDeposit += deposit
			totalBalanceDue += balance
			totalChargedNow += deposit
			
			res.PreOrder = append(res.PreOrder, cItem)
		} else {
			totalShipReady += subtotal
			totalChargedNow += subtotal
			res.ShipReady = append(res.ShipReady, cItem)
		}
	}

	res.Summary = CartSummaryDTO{
		TotalShipReady:  fmt.Sprintf("%.2f", totalShipReady),
		TotalPreOrder:   fmt.Sprintf("%.2f", totalPreOrder),
		TotalDeposit:    fmt.Sprintf("%.2f", totalDeposit),
		TotalBalanceDue: fmt.Sprintf("%.2f", totalBalanceDue),
		TotalChargedNow: fmt.Sprintf("%.2f", totalChargedNow),
		Currency:        "USD",
	}

	return res, nil
}

func (s *service) GetCartSummary(ctx context.Context, userID, sessionID *string) (*CartSummaryDTO, error) {
	res, err := s.GetCartResponse(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	return &res.Summary, nil
}

func (s *service) AddItem(ctx context.Context, userID, sessionID *string, req CartItemRequest) error {
	variant, err := s.productService.GetVariantByID(ctx, req.VariantID)
	if err != nil {
		return apierror.ErrNotFound
	}
	
	if string(variant.FulfillmentType) != req.FulfillmentType {
		return apierror.ErrBadRequest
	}

	var price float64
	fmt.Sscanf(variant.WSPrice, "%f", &price)

	cart, err := s.getOrCreateCart(ctx, userID, sessionID)
	if err != nil {
		return err
	}

	item := &CartItemModel{
		CartID:           cart.ID,
		ShopifyVariantID: req.VariantID,
		FulfillmentType:  req.FulfillmentType,
		Quantity:         req.Quantity,
		UnitPrice:        price,
	}

	return s.store.AddItem(ctx, item)
}

func (s *service) UpdateItemQuantity(ctx context.Context, userID, sessionID *string, itemID string, req UpdateCartItemRequest) error {
	if req.Quantity == 0 {
		return s.store.RemoveItem(ctx, itemID)
	}
	return s.store.UpdateItemQuantity(ctx, itemID, req.Quantity)
}

func (s *service) RemoveItem(ctx context.Context, userID, sessionID *string, itemID string) error {
	return s.store.RemoveItem(ctx, itemID)
}

func (s *service) ClearCart(ctx context.Context, userID, sessionID *string) error {
	cart, err := s.store.GetCart(ctx, userID, sessionID)
	if err != nil || cart == nil {
		return nil // nothing to clear
	}
	return s.store.ClearCart(ctx, cart.ID)
}

func (s *service) MergeCarts(ctx context.Context, userID string, guestSessionID string) error {
	guestCart, err := s.store.GetCart(ctx, nil, &guestSessionID)
	if err != nil {
		return apierror.ErrInternal
	}
	if guestCart == nil {
		return nil // no guest cart to merge
	}

	userCart, err := s.getOrCreateCart(ctx, &userID, nil)
	if err != nil {
		return apierror.ErrInternal
	}

	return s.store.MergeCarts(ctx, guestCart.ID, userCart.ID)
}
