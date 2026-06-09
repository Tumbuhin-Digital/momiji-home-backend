package checkout

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type CheckoutService interface {
	InitiateCheckout(ctx context.Context, userID, sessionID *string, req InitiateCheckoutRequest) (*InitiateCheckoutResponse, error)
	ValidateAddress(ctx context.Context, req ValidateAddressRequest) map[string]string
}

type service struct {
	cartService      cart.CartService
	shopifyCli       shopify.Client
	stockLockService StockLockService
	store            StockLockStore
	feURL            string
}

func NewCheckoutService(cartService cart.CartService, shopifyCli shopify.Client, stockLockService StockLockService, store StockLockStore, feURL string) CheckoutService {
	return &service{cartService: cartService, shopifyCli: shopifyCli, stockLockService: stockLockService, store: store, feURL: feURL}
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
	var lockReqs []LockRequest

	for _, item := range cartRes.ShipReady {
		lines = append(lines, shopify.CartLineInput{
			MerchandiseId: item.VariantID,
			Quantity:      item.Quantity,
		})
		lockReqs = append(lockReqs, LockRequest{
			ShopifyVariantID: item.VariantID,
			Quantity:         item.Quantity,
		})
	}

	if len(lockReqs) > 0 {
		if err := s.stockLockService.AcquireLocks(ctx, userID, sessionID, lockReqs); err != nil {
			return nil, err
		}
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

	buyerIdentity := &shopify.CartBuyerIdentityInput{}
	hasBuyerIdentity := false

	if req.Email != "" {
		buyerIdentity.Email = req.Email
		hasBuyerIdentity = true
	}
	if req.Phone != "" {
		buyerIdentity.Phone = req.Phone
		hasBuyerIdentity = true
	}

	if req.Address1 != "" || req.City != "" {
		buyerIdentity.DeliveryAddressPreferences = []shopify.CartDeliveryAddressInput{
			{
				DeliveryAddress: shopify.AddressInput{
					FirstName: req.FirstName,
					LastName:  req.LastName,
					Address1:  req.Address1,
					City:      req.City,
					Province:  req.State,
					Zip:       req.Zip,
					Country:   req.Country,
					Phone:     req.Phone,
				},
			},
		}
		hasBuyerIdentity = true
	}

	if hasBuyerIdentity {
		cartInput.BuyerIdentity = buyerIdentity
	}

	res, err := s.shopifyCli.CreateStorefrontCart(ctx, cartInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create shopify cart: %w", err)
	}

	checkoutUrl := res.CheckoutUrl
	if s.feURL != "" {
		checkoutUrl += "?return_to=" + url.QueryEscape(s.feURL)
	}

	return &InitiateCheckoutResponse{
		CheckoutUrl: checkoutUrl,
	}, nil
}

func (s *service) ValidateAddress(ctx context.Context, req ValidateAddressRequest) map[string]string {
	if !strings.EqualFold(req.Country, "US") && !strings.EqualFold(req.Country, "United States") {
		return nil
	}

	zipDetails, err := s.store.GetUSZipCodeDetails(ctx, req.Zip)
	if err != nil || zipDetails == nil {
		return map[string]string{"zip": "Invalid US ZIP code"}
	}

	errors := make(map[string]string)

	if !strings.EqualFold(req.City, zipDetails.City) {
		errors["city"] = "City does not match ZIP"
	}

	if !strings.EqualFold(req.State, zipDetails.StateAbbr) && !strings.EqualFold(req.State, zipDetails.StateName) {
		errors["state"] = "State does not match ZIP"
	}

	if len(errors) > 0 {
		return errors
	}

	return nil
}
