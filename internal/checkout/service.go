package checkout

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"log/slog"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type CheckoutService interface {
	InitiateCheckout(ctx context.Context, userID, sessionID *string, req InitiateCheckoutRequest) (*InitiateCheckoutResponse, error)
	ValidateAddress(ctx context.Context, req ValidateAddressRequest) map[string]string
	GetShippingRates(ctx context.Context, userID, sessionID *string, req ShippingRatesRequest) ([]ShippingRateDTO, error)
}

type service struct {
	cartService      cart.CartService
	shopifyCli       shopify.Client
	stockLockService StockLockService
	store            StockLockStore
	feURL            string
	shipstationCli   shipstation.Client
	shipstationCfg   config.ShipStationConfig
}

func NewCheckoutService(cartService cart.CartService, shopifyCli shopify.Client, stockLockService StockLockService, store StockLockStore, feURL string, shipstationCli shipstation.Client, shipstationCfg config.ShipStationConfig) CheckoutService {
	return &service{
		cartService:      cartService,
		shopifyCli:       shopifyCli,
		stockLockService: stockLockService,
		store:            store,
		feURL:            feURL,
		shipstationCli:   shipstationCli,
		shipstationCfg:   shipstationCfg,
	}
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
		var discount *shopify.DraftOrderAppliedDiscountInput
		if item.RetailPrice != "" {
			retailPrice, err1 := strconv.ParseFloat(item.RetailPrice, 64)
			wsPrice, err2 := strconv.ParseFloat(item.UnitPrice, 64)
			if err1 == nil && err2 == nil && retailPrice > wsPrice {
				discountPerUnit := retailPrice - wsPrice
				discountPerUnit = math.Round(discountPerUnit*100) / 100
				discount = &shopify.DraftOrderAppliedDiscountInput{
					Title:     "Wholesale Pricing",
					Value:     discountPerUnit,
					ValueType: "FIXED_AMOUNT",
					Amount:    discountPerUnit,
				}
			}
		}

		draftLines = append(draftLines, shopify.DraftOrderLineItem{
			VariantID:         item.VariantID,
			Quantity:          item.Quantity,
			OriginalUnitPrice: item.UnitPrice,
			AppliedDiscount:   discount,
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
				matchedRate := s.matchShippingRate(rates, req.ShippingMethod)
				draftInput.ShippingLine = &shopify.ShippingLineInput{
					Title: matchedRate.Label,
					Price: matchedRate.Cost,
				}
			}
		}
	}

	res, err := s.shopifyCli.CreateDraftOrder(ctx, draftInput)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create shopify draft order", slog.Any("error", err), slog.String("checkout_reference", checkoutRef))
		return nil, fmt.Errorf("failed to create shopify draft order: %w", err)
	}

	slog.InfoContext(ctx, "Shopify draft order created", slog.String("checkout_reference", checkoutRef), slog.String("invoice_url", res.InvoiceUrl))

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

func (s *service) groundServiceCode() string {
	return s.shipstationCfg.GroundServiceCode
}

func (s *service) matchShippingRate(rates []ShippingRateDTO, shippingMethod string) *ShippingRateDTO {
	groundCode := s.groundServiceCode()
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
	packages := s.buildPackagesFromItems(items)
	if len(packages) == 0 {
		return []ShippingRateDTO{}, nil
	}

	if rateReq.Name == "" {
		rateReq.Name = "Recipient"
	}
	if rateReq.Phone == "" {
		rateReq.Phone = "555-555-5555"
	}
	if rateReq.Address1 == "" {
		rateReq.Address1 = "123 Unknown St"
	}

	req := shipstation.RateRequest{
		RateOptions: shipstation.RateOptions{
			CarrierIDs:   s.shipstationCfg.CarrierCodes,
			ServiceCodes: []string{s.groundServiceCode()},
		},
		Shipment: shipstation.Shipment{
			ValidateAddress: "no_validation",
			ShipFrom: shipstation.Address{
				Name:          s.shipstationCfg.WarehouseName,
				Phone:         s.shipstationCfg.WarehousePhone,
				AddressLine1:  s.shipstationCfg.WarehouseAddress1,
				CityLocality:  s.shipstationCfg.WarehouseCity,
				StateProvince: s.getStateAbbr(ctx, s.shipstationCfg.WarehouseCountry, s.shipstationCfg.WarehouseZip, s.shipstationCfg.WarehouseState),
				PostalCode:    s.shipstationCfg.WarehouseZip,
				CountryCode:   s.shipstationCfg.WarehouseCountry,
			},
			ShipTo: shipstation.Address{
				Name:          rateReq.Name,
				Phone:         rateReq.Phone,
				AddressLine1:  rateReq.Address1,
				CityLocality:  rateReq.City,
				StateProvince: s.getStateAbbr(ctx, rateReq.Country, rateReq.Zip, rateReq.State),
				PostalCode:    rateReq.Zip,
				CountryCode:   rateReq.Country,
			},
			Packages: packages,
		},
	}

	rates, err := s.shipstationCli.GetRates(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get shipping rates from shipstation", "error", err)
		return nil, apierror.New(500, "shipping_rate_error", "Failed to fetch shipping rates from carriers")
	}

	slog.InfoContext(ctx, "shipstation rates response", "count", len(rates), "rates", rates)

	dtos := s.filterGroundRates(rates)
	if len(dtos) == 0 {
		return nil, apierror.New(500, "shipping_rate_error", "Failed to fetch shipping rates from carriers")
	}

	return dtos, nil
}

// buildPackagesFromItems creates one ShipStation package per unit (quantity N = N identical packages).
func (s *service) buildPackagesFromItems(items []cart.CartItem) []shipstation.Package {
	const cmToInch = 0.393701
	var packages []shipstation.Package

	for _, item := range items {
		wt := item.Weight
		switch item.WeightUnit {
		case "KILOGRAMS":
			wt = wt * 2.20462
		case "GRAMS":
			wt = wt * 0.00220462
		}
		if wt == 0 {
			wt = 1.0
		}

		pkg := shipstation.Package{
			Weight: shipstation.Weight{
				Value: wt,
				Unit:  "pound",
			},
		}

		if item.Length > 0 || item.Width > 0 || item.Height > 0 {
			dims := []float64{item.Length, item.Width, item.Height}
			sort.Sort(sort.Reverse(sort.Float64Slice(dims)))
			pkg.Dimensions = &shipstation.Dimensions{
				Unit:   "inch",
				Length: math.Round(dims[0]*cmToInch*100) / 100,
				Width:  math.Round(dims[1]*cmToInch*100) / 100,
				Height: math.Round(dims[2]*cmToInch*100) / 100,
			}
		}

		qty := item.Quantity
		if qty < 1 {
			qty = 1
		}
		for i := 0; i < qty; i++ {
			packages = append(packages, pkg)
		}
	}

	return packages
}

func (s *service) filterGroundRates(rates []shipstation.Rate) []ShippingRateDTO {
	groundCode := s.groundServiceCode()
	var dtos []ShippingRateDTO
	for _, r := range rates {
		if r.ServiceCode != groundCode {
			continue
		}
		dtos = append(dtos, ShippingRateDTO{
			ServiceCode:  r.ServiceCode,
			Label:        r.ServiceType,
			Cost:         fmt.Sprintf("%.2f", r.ShippingAmount.Amount),
			Currency:     r.ShippingAmount.Currency,
			DeliveryDays: r.DeliveryDays,
		})
	}
	return dtos
}

func (s *service) getStateAbbr(ctx context.Context, country, zip, defaultState string) string {
	if !strings.EqualFold(country, "US") && !strings.EqualFold(country, "United States") {
		return defaultState
	}
	cleanZip := strings.TrimSpace(zip)
	zipDetails, err := s.store.GetUSZipCodeDetails(ctx, cleanZip)
	if err == nil && zipDetails != nil && zipDetails.StateAbbr != "" {
		return zipDetails.StateAbbr
	}
	if len(cleanZip) > 5 {
		zip5 := cleanZip[:5]
		zipDetails, err = s.store.GetUSZipCodeDetails(ctx, zip5)
		if err == nil && zipDetails != nil && zipDetails.StateAbbr != "" {
			return zipDetails.StateAbbr
		}
	}
	return defaultState
}
