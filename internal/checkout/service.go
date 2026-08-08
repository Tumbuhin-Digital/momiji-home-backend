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
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorderbatch"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/settings"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
)

type CheckoutService interface {
	InitiateCheckout(ctx context.Context, userID, sessionID *string, req InitiateCheckoutRequest) (*InitiateCheckoutResponse, error)
	CreateManualOrder(ctx context.Context, req ManualOrderRequest) (*ManualOrderResponse, error)
	ReleaseCheckout(ctx context.Context, userID, sessionID *string, checkoutReference *string) error
	ValidateAddress(ctx context.Context, req ValidateAddressRequest) map[string]string
	GetShippingRates(ctx context.Context, userID, sessionID *string, req ShippingRatesRequest) ([]ShippingRateDTO, error)
}

type service struct {
	cartService       cart.CartService
	productService    product.ProductService
	shopifyCli        shopify.Client
	stockLockService  StockLockService
	store             StockLockStore
	settingsStore     settings.SettingsStore
	feURL             string
	shipstationCli    shipstation.Client
	shipstationCfg    config.ShipStationConfig
	warehouseResolver warehouse.Resolver
	batchService      BatchPreviewer
	batchLockService  preorderbatch.BatchLockService
}

type BatchPreviewer interface {
	PreviewAllocation(ctx context.Context, shopifyVariantID string, qty int, userID, sessionID *string) (*preorderbatch.AllocateResult, error)
}

func NewCheckoutService(cartService cart.CartService, productService product.ProductService, shopifyCli shopify.Client, stockLockService StockLockService, store StockLockStore, settingsStore settings.SettingsStore, feURL string, shipstationCli shipstation.Client, shipstationCfg config.ShipStationConfig, warehouseResolver warehouse.Resolver) CheckoutService {
	return &service{
		cartService:       cartService,
		productService:    productService,
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

func (s *service) SetBatchPreviewer(batchService BatchPreviewer) {
	s.batchService = batchService
}

func (s *service) SetBatchLockService(batchLockService preorderbatch.BatchLockService) {
	s.batchLockService = batchLockService
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

	if err := validateShipTogether(cartRes, req.ShipTogether); err != nil {
		return nil, err
	}

	for _, item := range append(cartRes.ShipReady, cartRes.PreOrder...) {
		if err := s.productService.ValidateVariantActive(ctx, item.VariantID); err != nil {
			return nil, err
		}
	}

	// Pre-lock inventory reconcile only when ship-ready stock will actually be reserved.
	if !req.ShipTogether {
		if depleted, err := s.reconcileShipReadyInventory(ctx, userID, sessionID, cartRes.ShipReady); err != nil {
			return nil, err
		} else if depleted != nil {
			return nil, shipReadyInventoryDepletedError(depleted)
		}

		// Cart may have been unchanged; re-load so lock qty matches latest split.
		cartRes, err = s.cartService.GetCartResponse(ctx, userID, sessionID)
		if err != nil {
			return nil, apierror.ErrInternal
		}
		if len(cartRes.ShipReady) == 0 && len(cartRes.PreOrder) == 0 {
			return nil, apierror.New(400, "bad_request", "cart is empty")
		}
	}

	shipReadyItems, preOrderItems := applyShipTogetherSegments(cartRes.ShipReady, cartRes.PreOrder, req.ShipTogether)

	checkoutRef := uuid.NewString()

	var draftLines []shopify.DraftOrderLineItem
	var lockReqs []LockRequest

	for _, item := range shipReadyItems {
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
			if isOutOfStockError(err) {
				if depleted, recErr := s.reconcileShipReadyInventory(ctx, userID, sessionID, cartRes.ShipReady); recErr != nil {
					return nil, recErr
				} else if depleted != nil {
					return nil, shipReadyInventoryDepletedError(depleted)
				}
			}
			return nil, err
		}
		lockExpiresAt = expiresAt
	}

	for _, item := range preOrderItems {
		if s.batchService != nil {
			preview, err := s.batchService.PreviewAllocation(ctx, item.VariantID, item.Quantity, userID, sessionID)
			if err != nil {
				return nil, err
			}
			if preview != nil && preview.Depletion != nil && !req.AcceptBatchDepletion {
				return nil, apierror.NewWithDetails(409, "batch_depletion_confirmation_required", "Current pre-order batch has depleted", preview.Depletion)
			}
		}
		draftLines = append(draftLines, buildPreOrderDraftLine(item))
	}

	if s.batchLockService != nil && len(preOrderItems) > 0 {
		batchLockReqs := make([]preorderbatch.BatchLockRequest, 0, len(preOrderItems))
		for _, item := range preOrderItems {
			batchLockReqs = append(batchLockReqs, preorderbatch.BatchLockRequest{
				ShopifyVariantID: item.VariantID,
				Quantity:         item.Quantity,
				ProductTitle:     item.Title,
				ImageURL:         item.ImageSrc,
			})
		}
		expiresAt, err := s.batchLockService.AcquireBatchLocks(ctx, userID, sessionID, checkoutRef, batchLockReqs)
		if err != nil {
			_ = s.stockLockService.ReleaseLocksByCheckoutReference(ctx, checkoutRef)
			return nil, err
		}
		if lockExpiresAt.IsZero() || expiresAt.Before(lockExpiresAt) {
			if !expiresAt.IsZero() {
				lockExpiresAt = expiresAt
			}
		}
	}

	draftInput := shopify.DraftOrderInput{
		LineItems: draftLines,
	}

	if req.Email != "" {
		draftInput.Email = req.Email
	}

	slog.InfoContext(ctx, "Initiating checkout",
		slog.String("checkout_reference", checkoutRef),
		slog.Bool("ship_together", req.ShipTogether),
		slog.Int("ship_ready_items", len(shipReadyItems)),
		slog.Int("pre_order_items", len(preOrderItems)))

	draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
		Key: "checkout_reference", Value: checkoutRef,
	})
	draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.WholesaleSourceAttribute)

	if req.ShippingMethod != "" {
		draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
			Key: "preorder_shipping_method", Value: req.ShippingMethod,
		})
	}

	hasLtl := false
	for _, item := range append(shipReadyItems, preOrderItems...) {
		if item.IsLtl {
			hasLtl = true
			break
		}
	}
	if hasLtl {
		draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
			Key: "has_ltl", Value: "true",
		})
	}

	if len(preOrderItems) > 0 {
		preOrderOrigin := warehouse.CodeEast
		if s.warehouseResolver != nil {
			preOrderOrigin = s.warehouseResolver.ResolveOrigin("pre_order", req.Origin)
		}
		draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
			Key: "preorder_warehouse_origin", Value: preOrderOrigin,
		})
	}

	if req.ShipTogether {
		batchNames, err := collectCheckoutBatchNames(ctx, s.batchService, preOrderItems, userID, sessionID)
		if err != nil {
			_ = s.stockLockService.ReleaseLocksByCheckoutReference(ctx, checkoutRef)
			if s.batchLockService != nil {
				_ = s.batchLockService.ReleaseBatchLocksByCheckoutReference(ctx, checkoutRef)
			}
			return nil, err
		}
		draftInput.CustomAttributes = append(draftInput.CustomAttributes,
			shopify.AttributeInput{Key: "ship_together", Value: "true"},
		)
		if batchNames != "" {
			draftInput.CustomAttributes = append(draftInput.CustomAttributes,
				shopify.AttributeInput{Key: "hold_until_batch", Value: batchNames},
			)
		}
		draftInput.Note = formatShipTogetherHoldNote(batchNames)
	}

	if req.Address1 != "" || req.City != "" {
		shippingAddr := &shopify.AddressInput{
			FirstName: req.FirstName,
			LastName:  req.LastName,
			Company:   req.Company,
			Address1:  req.Address1,
			City:      req.City,
			Province:  req.State,
			Zip:       req.Zip,
			Country:   req.Country,
			Phone:     req.Phone,
		}
		draftInput.ShippingAddress = shippingAddr
		draftInput.BillingAddress = resolveBillingAddress(req, shippingAddr)
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
	if len(shipReadyItems) > 0 {
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
	// Skip when every pre-order line is LTL — freight is calculated manually by the team.
	if len(preOrderItems) > 0 {
		allPreOrderLtl := true
		for _, item := range preOrderItems {
			if !item.IsLtl {
				allPreOrderLtl = false
				break
			}
		}
		if allPreOrderLtl {
			slog.InfoContext(ctx, "pre-order shipping estimate skipped: all LTL items",
				slog.String("checkout_reference", checkoutRef))
		} else if req.Zip == "" {
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
			// When ship_together, cart segment alone omits reclassified ship-ready weight.
			if req.ShipTogether {
				ratesReq.LineItems = toShippingRateLineItems(preOrderItems)
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
		_ = s.stockLockService.ReleaseLocksByCheckoutReference(ctx, checkoutRef)
		if s.batchLockService != nil {
			_ = s.batchLockService.ReleaseBatchLocksByCheckoutReference(ctx, checkoutRef)
		}
		slog.ErrorContext(ctx, "Failed to create shopify draft order", slog.Any("error", err), slog.String("checkout_reference", checkoutRef))
		return nil, fmt.Errorf("failed to create shopify draft order: %w", err)
	}

	sessionExpiresAt := lockExpiresAt
	if sessionExpiresAt.IsZero() {
		sessionExpiresAt = time.Now().Add(stockLockTTL)
	}
	if err := s.store.UpsertCheckoutSession(ctx, &CheckoutSession{
		CheckoutReference:   checkoutRef,
		ShopifyDraftOrderID: res.ID,
		ExpiresAt:           sessionExpiresAt,
		UserID:              userID,
		SessionID:           sessionID,
	}); err != nil {
		_ = s.shopifyCli.DeleteDraftOrder(ctx, res.ID)
		_ = s.stockLockService.ReleaseLocksByCheckoutReference(ctx, checkoutRef)
		if s.batchLockService != nil {
			_ = s.batchLockService.ReleaseBatchLocksByCheckoutReference(ctx, checkoutRef)
		}
		slog.ErrorContext(ctx, "Failed to persist checkout session", slog.Any("error", err), slog.String("checkout_reference", checkoutRef))
		return nil, fmt.Errorf("failed to persist checkout session: %w", err)
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
	if checkoutReference != nil && *checkoutReference != "" {
		if err := s.cancelCheckoutDraft(ctx, *checkoutReference); err != nil {
			slog.WarnContext(ctx, "Failed to delete Shopify draft on checkout release",
				slog.String("checkout_reference", *checkoutReference),
				slog.Any("error", err))
		}
	}

	if err := s.stockLockService.ReleaseLocksForIdentity(ctx, userID, sessionID, checkoutReference); err != nil {
		return err
	}
	if s.batchLockService != nil {
		if err := s.batchLockService.ReleaseBatchLocksForIdentity(ctx, userID, sessionID, checkoutReference); err != nil {
			return err
		}
	}

	if checkoutReference != nil && *checkoutReference != "" {
		if err := s.store.DeleteCheckoutSession(ctx, *checkoutReference); err != nil {
			slog.WarnContext(ctx, "Failed to delete checkout session after release",
				slog.String("checkout_reference", *checkoutReference),
				slog.Any("error", err))
		}
	}
	return nil
}

func (s *service) cancelCheckoutDraft(ctx context.Context, checkoutReference string) error {
	session, err := s.store.GetCheckoutSession(ctx, checkoutReference)
	if err != nil {
		return err
	}
	if session == nil || session.ShopifyDraftOrderID == "" {
		return nil
	}
	return s.shopifyCli.DeleteDraftOrder(ctx, session.ShopifyDraftOrderID)
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
	var items []cart.CartItem
	if len(rateReq.LineItems) > 0 {
		resolved, err := s.resolveExplicitLineItems(ctx, rateReq.LineItems)
		if err != nil {
			return nil, err
		}
		items = resolved
	} else {
		cartRes, err := s.cartService.GetCartResponse(ctx, userID, sessionID)
		if err != nil {
			return nil, apierror.ErrInternal
		}
		items = s.resolveCartItems(cartRes, rateReq.Segment)
	}

	if len(items) == 0 {
		return []ShippingRateDTO{}, nil
	}

	return s.calculateRatesForItems(ctx, items, rateReq)
}

func (s *service) resolveExplicitLineItems(ctx context.Context, lineItems []ShippingRateLineItem) ([]cart.CartItem, error) {
	var items []cart.CartItem
	for _, li := range lineItems {
		variant, err := s.productService.GetVariantByID(ctx, li.VariantID)
		if err != nil {
			return nil, apierror.New(404, "not_found", "Variant not found: "+li.VariantID)
		}
		unitPrice := 0.0
		fmt.Sscanf(variant.WSPrice, "%f", &unitPrice)
		if unitPrice <= 0 {
			fmt.Sscanf(variant.RetailPrice, "%f", &unitPrice)
		}
		items = append(items, cart.CartItem{
			VariantID:         variant.ID,
			Title:             variant.Title,
			ImageSrc:          variant.ImageSrc,
			Quantity:          li.Quantity,
			InventoryQuantity: variant.InventoryQuantity,
			UnitPrice:         fmt.Sprintf("%.2f", unitPrice),
			RetailPrice:       variant.RetailPrice,
			Subtotal:          fmt.Sprintf("%.2f", unitPrice*float64(li.Quantity)),
			Weight:            variant.WeightKg,
			WeightUnit:        "KILOGRAMS",
			Length:            variant.LengthCm,
			Width:             variant.WidthCm,
			Height:            variant.HeightCm,
			IsLtl:             variant.IsLtl,
		})
	}
	return items, nil
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
		if item.IsLtl {
			continue
		}
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

	baseCost, bufferAmount, bufferedCost := shipping.ApplyShippingBuffer(amount)

	return []ShippingRateDTO{{
		ServiceCode:  groundCode,
		Label:        "Ground",
		BaseCost:     fmt.Sprintf("%.2f", baseCost),
		BufferAmount: fmt.Sprintf("%.2f", bufferAmount),
		Cost:         fmt.Sprintf("%.2f", bufferedCost),
		Currency:     currency,
	}}, nil
}

func (s *service) zipLookup(ctx context.Context, zip string) (string, bool) {
	zipDetails, err := s.store.GetUSZipCodeDetails(ctx, zip)
	if err != nil || zipDetails == nil || zipDetails.StateAbbr == "" {
		return "", false
	}
	return zipDetails.StateAbbr, true
}
