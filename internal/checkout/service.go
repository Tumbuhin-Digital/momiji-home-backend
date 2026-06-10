package checkout

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
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

	var draftLines []shopify.DraftOrderLineItem
	var lockReqs []LockRequest

	for _, item := range cartRes.ShipReady {
		draftLines = append(draftLines, shopify.DraftOrderLineItem{
			VariantID:         item.VariantID,
			Quantity:          item.Quantity,
			OriginalUnitPrice: item.UnitPrice,
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
		unitPrice, _ := strconv.ParseFloat(item.UnitPrice, 64)
		deposit := unitPrice * 0.5
		
		requiresShipping := false
		
		draftLine := shopify.DraftOrderLineItem{
			Title:             fmt.Sprintf("[PREORDER] %s (DP 50%%)", item.Title),
			Quantity:          item.Quantity,
			OriginalUnitPrice: fmt.Sprintf("%.2f", deposit),
			RequiresShipping:  &requiresShipping,
			CustomAttributes: []shopify.AttributeInput{
				{Key: "type", Value: "preorder_dp"},
				{Key: "full_price", Value: fmt.Sprintf("%.2f", unitPrice)},
				{Key: "variant_ref", Value: item.VariantID},
			},
		}

		if item.Weight > 0 {
			draftLine.Weight = &shopify.DraftOrderLineItemWeightInput{
				Unit:  "KILOGRAMS",
				Value: item.Weight,
			}
		}

		draftLines = append(draftLines, draftLine)
	}

	draftInput := shopify.DraftOrderInput{
		LineItems: draftLines,
	}

	if req.Email != "" {
		draftInput.Email = req.Email
	}

	checkoutRef := uuid.NewString()

	draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
		Key: "checkout_reference", Value: checkoutRef,
	})

	if req.ShippingMethod != "" {
		draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
			Key: "preorder_shipping_method", Value: req.ShippingMethod,
		})
	}

	if req.Address1 != "" || req.City != "" {
		draftInput.ShippingAddress = &shopify.AddressInput{
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Address1:  req.Address1,
			City:      req.City,
			Province:  req.State,
			Zip:       req.Zip,
			Country:   req.Country,
			Phone:     req.Phone,
		}
	}

	res, err := s.shopifyCli.CreateDraftOrder(ctx, draftInput)
	if err != nil {
		return nil, fmt.Errorf("failed to create shopify draft order: %w", err)
	}

	checkoutUrl := res.InvoiceUrl
	if s.feURL != "" {
		checkoutUrl += "?return_to=" + url.QueryEscape(s.feURL)
	}

	return &InitiateCheckoutResponse{
		CheckoutUrl:       checkoutUrl,
		CheckoutReference: checkoutRef,
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
