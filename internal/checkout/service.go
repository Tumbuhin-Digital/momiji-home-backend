package checkout

import (
	"context"
	"fmt"
	"strconv"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type CheckoutService interface {
	InitiateCheckout(ctx context.Context, userID, sessionID *string, req InitiateCheckoutRequest) (*InitiateCheckoutResponse, error)
}

type service struct {
	cartService cart.CartService
	shopifyCli  shopify.Client
}

func NewCheckoutService(cartService cart.CartService, shopifyCli shopify.Client) CheckoutService {
	return &service{cartService: cartService, shopifyCli: shopifyCli}
}

func (s *service) InitiateCheckout(ctx context.Context, userID, sessionID *string, req InitiateCheckoutRequest) (*InitiateCheckoutResponse, error) {
	cartRes, err := s.cartService.GetCartResponse(ctx, userID, sessionID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	if len(cartRes.ShipReady) == 0 && len(cartRes.PreOrder) == 0 {
		return nil, apierror.New(400, "bad_request", "cart is empty")
	}

	var lines []shopify.CartLineInput

	for _, item := range cartRes.ShipReady {
		lines = append(lines, shopify.CartLineInput{
			MerchandiseId: item.VariantID,
			Quantity:      item.Quantity,
		})
	}

	for _, item := range cartRes.PreOrder {
		// Calculate 50% deposit
		unitPrice, _ := strconv.ParseFloat(item.UnitPrice, 64)
		deposit := unitPrice * 0.5

		// Storefront Cart API allows overriding price using _customPrice attribute if configured, 
		// but standard attributes just attach data. We'll add the attribute for tracking.
		// NOTE: Actual Shopify custom pricing in carts requires Shopify Scripts or B2B APIs,
		// or passing custom attributes and modifying the price via Scripts.
		lines = append(lines, shopify.CartLineInput{
			MerchandiseId: item.VariantID,
			Quantity:      item.Quantity,
			Attributes: []shopify.AttributeInput{
				{Key: "_is_preorder", Value: "true"},
				{Key: "_deposit_price", Value: fmt.Sprintf("%.2f", deposit)},
			},
		})
	}

	cartInput := shopify.CartCreateInput{
		Lines: lines,
	}

	if req.Email != "" {
		cartInput.BuyerIdentity = &shopify.CartBuyerIdentityInput{Email: req.Email}
	}

	res, err := s.shopifyCli.CreateStorefrontCart(ctx, cartInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create shopify cart: %w", err)
	}

	return &InitiateCheckoutResponse{
		CheckoutUrl: res.CheckoutUrl,
	}, nil
}
