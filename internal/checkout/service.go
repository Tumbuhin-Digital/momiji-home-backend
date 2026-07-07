package checkout

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/settings"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
)

type CheckoutService interface {
	InitiateCheckout(ctx context.Context, userID, sessionID *string, req InitiateCheckoutRequest) (*InitiateCheckoutResponse, error)
	ReleaseCheckout(ctx context.Context, userID, sessionID *string, checkoutReference *string) error
	ValidateAddress(ctx context.Context, req ValidateAddressRequest) map[string]string
	GetShippingRates(ctx context.Context, userID, sessionID *string, req ShippingRatesRequest) ([]ShippingRateDTO, error)
}

type service struct {
	cartService       cart.CartService
	shopifyCli        shopify.Client
	stockLockService  StockLockService
	store             StockLockStore
	settingsStore     settings.SettingsStore
	feURL             string
	shipstationCli    shipstation.Client
	shipstationCfg    config.ShipStationConfig
	warehouseResolver warehouse.Resolver
}

func NewCheckoutService(cartService cart.CartService, shopifyCli shopify.Client, stockLockService StockLockService, store StockLockStore, settingsStore settings.SettingsStore, feURL string, shipstationCli shipstation.Client, shipstationCfg config.ShipStationConfig, warehouseResolver warehouse.Resolver) CheckoutService {
	return &service{
		cartService:       cartService,
		shopifyCli:        shopifyCli,
		stockLockService:  stockLockService,
		store:             store,
		settingsStore:     settingsStore,
		feURL:             feURL,
		shipstationCli:    shipstationCli,
		shipstationCfg:    shipstationCfg,
		warehouseResolver: warehouseResolver,
	}
}

func (s *service) InitiateCheckout(ctx context.Context, userID, sessionID *string, req InitiateCheckoutRequest) (*InitiateCheckoutResponse, error) {
	if isClosed, message := s.getStoreClosedStatus(ctx); isClosed {
		return nil, apierror.New(403, "store_closed", message)
	}

	cartRes, err := s.cartService.GetCartResponse(ctx, userID, sessionID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	if len(cartRes.ShipReady) == 0 && len(cartRes.PreOrder) == 0 {
		return nil, apierror.New(400, "bad_request", "cart is empty")
	}

	checkoutRef := uuid.NewString()

	var draftLines []shopify.DraftOrderLineItem
	var lockReqs []LockRequest

	for _, item := range cartRes.ShipReady {
		draftLine := buildShipReadyDraftLine(item)
		draftLines = append(draftLines, draftLine)
		lockReqs = append(lockReqs, LockRequest{
			ShopifyVariantID: item.VariantID,
			Quantity:         item.Quantity,
		})
	}

	var lockExpiresAt time.Time
	if len(lockReqs) > 0 {
		expiresAt, err := s.stockLockService.AcquireLocks(ctx, userID, sessionID, checkoutRef, lockReqs)
		if err != nil {
			return nil, err
		}
		lockExpiresAt = expiresAt
	}

	for _, item := range cartRes.PreOrder {
		unitPrice, _ := strconv.ParseFloat(item.UnitPrice, 64)
		deposit := unitPrice * 0.5

		requiresShipping := false

		draftLine := shopify.DraftOrderLineItem{
			Title:             fmt.Sprintf("[PREORDER] %s (Deposit 50%%)", item.Title),
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

	slog.InfoContext(ctx, "Initiating checkout", slog.String("checkout_reference", checkoutRef), slog.Int("ship_ready_items", len(cartRes.ShipReady)), slog.Int("pre_order_items", len(cartRes.PreOrder)))

	draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
		Key: "checkout_reference", Value: checkoutRef,
	})
	draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.WholesaleSourceAttribute)

	if req.ShippingMethod != "" {
		draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
			Key: "preorder_shipping_method", Value: req.ShippingMethod,
		})
	}

	if len(cartRes.PreOrder) > 0 {
		preOrderOrigin := warehouse.CodeEast
		if s.warehouseResolver != nil {
			preOrderOrigin = s.warehouseResolver.ResolveOrigin("pre_order", req.Origin)
		}
		draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
			Key: "preorder_warehouse_origin", Value: preOrderOrigin,
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
		if snap := ShippingAddressSnapshotFromRequest(req); snap != nil {
			if jsonVal, err := snap.JSON(); err != nil {
				slog.WarnContext(ctx, "failed to marshal checkout shipping address snapshot",
					slog.String("checkout_reference", checkoutRef),
					slog.Any("error", err))
			} else {
				draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
					Key: ShippingAddressNoteAttribute, Value: jsonVal,
				})
			}
		}
	}

	// Set shipping line for ship-ready items using live ShipStation rates
	if len(cartRes.ShipReady) > 0 {
		if req.Zip == "" {
			slog.WarnContext(ctx, "ship ready shipping skipped: zip code missing",
				slog.String("checkout_reference", checkoutRef))
		} else {
			country := req.Country
			if country == "" {
				country = "US"
			}
			ratesReq := ShippingRatesRequest{
				Name:     strings.TrimSpace(req.FirstName + " " + req.LastName),
				Phone:    req.Phone,
				Address1: req.Address1,
				City:     req.City,
				State:    req.State,
				Zip:      req.Zip,
				Country:  country,
				Segment:  "ship_ready",
			}
			rates, rateErr := s.GetShippingRates(ctx, userID, sessionID, ratesReq)
			if rateErr != nil {
				slog.WarnContext(ctx, "ship ready shipping rate lookup failed",
					slog.String("checkout_reference", checkoutRef),
					slog.Any("error", rateErr))
			} else if len(rates) == 0 {
				slog.WarnContext(ctx, "no ship ready shipping rates returned",
					slog.String("checkout_reference", checkoutRef))
			} else {
				matchedRate := s.matchShippingRate(rates, req.ShippingMethod, warehouse.CodeEast)
				if matchedRate != nil {
					draftInput.ShippingLine = shopify.NewShippingLineInput(
						matchedRate.Label,
						matchedRate.Cost,
						"USD",
					)
				}
			}
		}
	}

	// Persist pre-order shipping estimate for admin fulfillment (balance-due invoice).
	if len(cartRes.PreOrder) > 0 {
		if req.Zip == "" {
			slog.WarnContext(ctx, "pre-order shipping estimate skipped: zip code missing",
				slog.String("checkout_reference", checkoutRef))
		} else {
			country := req.Country
			if country == "" {
				country = "US"
			}
			ratesReq := ShippingRatesRequest{
				Name:     strings.TrimSpace(req.FirstName + " " + req.LastName),
				Phone:    req.Phone,
				Address1: req.Address1,
				City:     req.City,
				State:    req.State,
				Zip:      req.Zip,
				Country:  country,
				Segment:  "pre_order",
				Origin:   req.Origin,
			}
			rates, rateErr := s.GetShippingRates(ctx, userID, sessionID, ratesReq)
			if rateErr != nil {
				slog.WarnContext(ctx, "pre-order shipping rate lookup failed",
					slog.String("checkout_reference", checkoutRef),
					slog.Any("error", rateErr))
			} else if len(rates) == 0 {
				slog.WarnContext(ctx, "no pre-order shipping rates returned",
					slog.String("checkout_reference", checkoutRef))
			} else {
				preOrderOrigin := warehouse.CodeEast
				if s.warehouseResolver != nil {
					preOrderOrigin = s.warehouseResolver.ResolveOrigin("pre_order", req.Origin)
				}
				matchedRate := s.matchShippingRate(rates, req.ShippingMethod, preOrderOrigin)
				if matchedRate != nil {
					draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
						Key: "preorder_shipping_estimate", Value: matchedRate.Cost,
					})
				}
			}
		}
	}

	res, err := s.shopifyCli.CreateDraftOrder(ctx, draftInput)
	if err != nil {
		if len(lockReqs) > 0 {
			_ = s.stockLockService.ReleaseLocksByCheckoutReference(ctx, checkoutRef)
		}
		slog.ErrorContext(ctx, "Failed to create shopify draft order", slog.Any("error", err), slog.String("checkout_reference", checkoutRef))
		return nil, fmt.Errorf("failed to create shopify draft order: %w", err)
	}

	slog.InfoContext(ctx, "Shopify draft order created", slog.String("checkout_reference", checkoutRef), slog.String("invoice_url", res.InvoiceUrl))

	checkoutUrl := res.InvoiceUrl
	if s.feURL != "" {
		checkoutUrl += "?return_to=" + url.QueryEscape(s.feURL)
	}

	resp := &InitiateCheckoutResponse{
		CheckoutUrl:       checkoutUrl,
		CheckoutReference: checkoutRef,
	}
	if !lockExpiresAt.IsZero() {
		resp.ExpiresAt = lockExpiresAt.UTC().Format(time.RFC3339)
	}

	return resp, nil
}

func (s *service) ReleaseCheckout(ctx context.Context, userID, sessionID *string, checkoutReference *string) error {
	return s.stockLockService.ReleaseLocksForIdentity(ctx, userID, sessionID, checkoutReference)
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

func (s *service) getStoreClosedStatus(ctx context.Context) (bool, string) {
	const defaultMessage = "Store is currently closed. Checkout is temporarily unavailable."

	if s.settingsStore == nil {
		return false, defaultMessage
	}

	storeClosedRaw, err := s.settingsStore.Get(ctx, settings.KeyStoreClosed)
	if err != nil {
		return false, defaultMessage
	}

	isClosed, err := strconv.ParseBool(strings.TrimSpace(storeClosedRaw))
	if err != nil || !isClosed {
		return false, defaultMessage
	}

	message, err := s.settingsStore.Get(ctx, settings.KeyStoreClosedMessage)
	if err != nil {
		return true, defaultMessage
	}

	trimmedMessage := strings.TrimSpace(message)
	if trimmedMessage == "" {
		return true, defaultMessage
	}

	return true, trimmedMessage
}

func (s *service) GetShippingRates(ctx context.Context, userID, sessionID *string, rateReq ShippingRatesRequest) ([]ShippingRateDTO, error) {
	cartRes, err := s.cartService.GetCartResponse(ctx, userID, sessionID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	items := s.resolveCartItems(cartRes, rateReq.Segment)
	if len(items) == 0 {
		return []ShippingRateDTO{}, nil
	}

	return s.calculateRatesForItems(ctx, items, rateReq)
}

func (s *service) resolveCartItems(cartRes *cart.CartResponse, segment string) []cart.CartItem {
	switch segment {
	case "ship_ready":
		return cartRes.ShipReady
	case "pre_order":
		return cartRes.PreOrder
	default:
		if len(cartRes.PreOrder) > 0 {
			return cartRes.PreOrder
		}
		return cartRes.ShipReady
	}
}

func (s *service) groundServiceCode(originCode string) string {
	if s.warehouseResolver != nil {
		if origin, err := s.warehouseResolver.GetOrigin(context.Background(), originCode); err == nil && origin.GroundServiceCode != "" {
			return origin.GroundServiceCode
		}
	}
	if s.shipstationCfg.GroundServiceCode != "" {
		return s.shipstationCfg.GroundServiceCode
	}
	return "ups_ground"
}

func (s *service) matchShippingRate(rates []ShippingRateDTO, shippingMethod, originCode string) *ShippingRateDTO {
	groundCode := s.groundServiceCode(originCode)
	if shippingMethod != "" {
		for i := range rates {
			if rates[i].ServiceCode == shippingMethod {
				return &rates[i]
			}
		}
	}
	for i := range rates {
		if rates[i].ServiceCode == groundCode {
			return &rates[i]
		}
	}
	if len(rates) > 0 {
		return &rates[0]
	}
	return nil
}

func (s *service) calculateRatesForItems(ctx context.Context, items []cart.CartItem, rateReq ShippingRatesRequest) ([]ShippingRateDTO, error) {
	var units []shipping.PackableUnit
	for _, item := range items {
		qty := item.Quantity
		if qty < 1 {
			qty = 1
		}
		units = append(units, shipping.PackableUnitFromCartItem(
			item.Weight, item.WeightUnit, item.Length, item.Width, item.Height, qty,
		))
	}

	packages := shipping.BuildPackages(ctx, units)
	if len(packages) == 0 {
		return []ShippingRateDTO{}, nil
	}

	originCode := warehouse.CodeEast
	if s.warehouseResolver != nil {
		originCode = s.warehouseResolver.ResolveOrigin(rateReq.Segment, rateReq.Origin)
	}

	if s.warehouseResolver == nil {
		return nil, apierror.New(500, "shipping_rate_error", "Warehouse resolver not configured")
	}

	origin, err := s.warehouseResolver.GetOrigin(ctx, originCode)
	if err != nil {
		return nil, apierror.New(500, "shipping_rate_error", "Failed to resolve warehouse origin")
	}

	amount, currency, err := shipping.CalculateGroundRate(
		ctx,
		s.shipstationCli,
		shipping.ShipFromAddress{
			Name:     origin.Name,
			Phone:    origin.Phone,
			Address1: origin.Address1,
			City:     origin.City,
			State:    origin.State,
			Zip:      origin.Zip,
			Country:  origin.Country,
		},
		s.shipstationCfg.CarrierCodes,
		origin.GroundServiceCode,
		shipping.ShipToAddress{
			Name:     rateReq.Name,
			Phone:    rateReq.Phone,
			Address1: rateReq.Address1,
			City:     rateReq.City,
			State:    rateReq.State,
			Zip:      rateReq.Zip,
			Country:  rateReq.Country,
		},
		packages,
		s.zipLookup,
	)
	if err != nil {
		return nil, apierror.New(500, "shipping_rate_error", "Failed to fetch shipping rates from carriers")
	}

	groundCode := s.groundServiceCode(originCode)
	if groundCode == "" {
		groundCode = "ups_ground"
	}

	return []ShippingRateDTO{{
		ServiceCode: groundCode,
		Label:       "Ground",
		Cost:        fmt.Sprintf("%.2f", amount),
		Currency:    currency,
	}}, nil
}

func (s *service) zipLookup(ctx context.Context, zip string) (string, bool) {
	zipDetails, err := s.store.GetUSZipCodeDetails(ctx, zip)
	if err != nil || zipDetails == nil || zipDetails.StateAbbr == "" {
		return "", false
	}
	return zipDetails.StateAbbr, true
}
