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

	// Calculate and set shipping cost for Shopify draft order
	shippingCost := 20.00
	shippingTitle := "Standard Shipping"
	if req.Zip != "" {
		ratesReq := ShippingRatesRequest{
			Zip:     req.Zip,
			Country: req.Country,
		}
		if ratesReq.Country == "" {
			ratesReq.Country = "US"
		}
		rates, rateErr := s.GetShippingRates(ctx, userID, sessionID, ratesReq)
		if rateErr == nil && len(rates) > 0 {
			var matchedRate *ShippingRateDTO
			for _, r := range rates {
				if r.ServiceCode == req.ShippingMethod {
					matchedRate = &r
					break
				}
			}
			if matchedRate == nil && req.ShippingMethod == "" {
				matchedRate = &rates[0]
			}
			if matchedRate != nil {
				if cost, err := strconv.ParseFloat(matchedRate.Cost, 64); err == nil {
					shippingCost = cost
				}
				shippingTitle = matchedRate.Label
			}
		}
	} else if req.ShippingMethod == "expedited" {
		shippingCost = 35.00
		shippingTitle = "Expedited Shipping"
	}

	// Only apply shipping line if there is shipping required (e.g. ship ready or pre-order items present)
	if len(cartRes.ShipReady) > 0 || len(cartRes.PreOrder) > 0 {
		draftInput.ShippingLine = &shopify.ShippingLineInput{
			Title: shippingTitle,
			Price: fmt.Sprintf("%.2f", shippingCost),
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

	if len(cartRes.PreOrder) == 0 {
		return []ShippingRateDTO{}, nil
	}

	totalWeight := 0.0
	var maxLen, maxWid, maxHt float64
	for _, item := range cartRes.PreOrder {
		wt := item.Weight
		if item.WeightUnit == "KILOGRAMS" {
			wt = wt * 2.20462
		} else if item.WeightUnit == "GRAMS" {
			wt = wt * 0.00220462
		}
		totalWeight += (wt * float64(item.Quantity))

		if item.Length > maxLen {
			maxLen = item.Length
		}
		if item.Width > maxWid {
			maxWid = item.Width
		}
		if item.Height > maxHt {
			maxHt = item.Height
		}
	}

	if totalWeight == 0 {
		totalWeight = 1.0 // default 1 lb
	}

	dims := []float64{maxLen, maxWid, maxHt}
	sort.Sort(sort.Reverse(sort.Float64Slice(dims)))
	pkgLength, pkgWidth, pkgHeight := dims[0], dims[1], dims[2]

	const cmToInch = 0.393701
	pkgLength = math.Round(pkgLength*cmToInch*100) / 100
	pkgWidth = math.Round(pkgWidth*cmToInch*100) / 100
	pkgHeight = math.Round(pkgHeight*cmToInch*100) / 100

	// Default fallback for required recipient details
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
			CarrierIDs: s.shipstationCfg.CarrierCodes,
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
			Packages: []shipstation.Package{
				{
					Weight: shipstation.Weight{
						Value: totalWeight,
						Unit:  "pound",
					},
					Dimensions: &shipstation.Dimensions{
						Unit:   "inch",
						Length: pkgLength,
						Width:  pkgWidth,
						Height: pkgHeight,
					},
				},
			},
		},
	}

	rates, err := s.shipstationCli.GetRates(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "failed to get shipping rates from shipstation", "error", err)
		return nil, apierror.New(500, "shipping_rate_error", "Failed to fetch shipping rates from carriers")
	}

	slog.InfoContext(ctx, "shipstation rates response", "count", len(rates), "rates", rates)

	var dtos []ShippingRateDTO
	for _, r := range rates {
		dtos = append(dtos, ShippingRateDTO{
			ServiceCode:  r.ServiceCode,
			Label:        r.ServiceType,
			Cost:         fmt.Sprintf("%.2f", r.ShippingAmount.Amount),
			Currency:     r.ShippingAmount.Currency,
			DeliveryDays: r.DeliveryDays,
		})
	}

	if len(dtos) == 0 {
		return nil, apierror.New(500, "shipping_rate_error", "Failed to fetch shipping rates from carriers")
	}

	return dtos, nil
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
