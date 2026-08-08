package checkout

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
)

func (s *service) CreateManualOrder(ctx context.Context, req ManualOrderRequest) (*ManualOrderResponse, error) {
	if isClosed, message := s.getStoreClosedStatus(ctx); isClosed {
		return nil, apierror.New(403, "store_closed", message)
	}

	if len(req.LineItems) == 0 {
		return nil, apierror.New(400, "bad_request", "at least one line item is required")
	}

	shipReady, preOrder, err := s.splitManualLineItems(ctx, req.LineItems)
	if err != nil {
		return nil, err
	}
	if len(shipReady) == 0 && len(preOrder) == 0 {
		return nil, apierror.New(400, "bad_request", "no valid line items")
	}
	if err := validateShipTogetherSegments(shipReady, preOrder, req.ShipTogether); err != nil {
		return nil, err
	}

	shipReady, preOrder = applyShipTogetherSegments(shipReady, preOrder, req.ShipTogether)

	if len(preOrder) > 0 && strings.TrimSpace(req.ShippingMethod) == "" {
		return nil, apierror.New(400, "bad_request", "shipping_method is required when pre-order items are present")
	}

	for _, item := range append(shipReady, preOrder...) {
		if err := s.productService.ValidateVariantActive(ctx, item.VariantID); err != nil {
			return nil, err
		}
	}

	checkoutRef := uuid.NewString()
	draftLines := buildDraftLinesFromSegments(shipReady, preOrder)

	draftInput := shopify.DraftOrderInput{
		LineItems: draftLines,
		Email:     req.Email,
	}

	slog.InfoContext(ctx, "Creating manual order draft",
		slog.String("checkout_reference", checkoutRef),
		slog.Bool("ship_together", req.ShipTogether),
		slog.Int("ship_ready_items", len(shipReady)),
		slog.Int("pre_order_items", len(preOrder)))

	draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
		Key: "checkout_reference", Value: checkoutRef,
	})
	draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.WholesaleSourceAttribute)
	draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
		Key: "manual_order", Value: "true",
	})

	if req.ShippingMethod != "" {
		draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
			Key: "preorder_shipping_method", Value: req.ShippingMethod,
		})
	}

	if len(preOrder) > 0 {
		preOrderOrigin := warehouse.CodeEast
		if s.warehouseResolver != nil {
			preOrderOrigin = s.warehouseResolver.ResolveOrigin("pre_order", req.Origin)
		}
		draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
			Key: "preorder_warehouse_origin", Value: preOrderOrigin,
		})
	}

	if req.ShipTogether {
		draftInput.CustomAttributes = append(draftInput.CustomAttributes,
			shopify.AttributeInput{Key: "ship_together", Value: "true"},
		)
		batchNames, batchErr := collectCheckoutBatchNames(ctx, s.batchService, preOrder, nil, nil)
		if batchErr != nil {
			slog.WarnContext(ctx, "manual order ship_together batch name lookup failed",
				slog.String("checkout_reference", checkoutRef),
				slog.Any("error", batchErr))
		} else if batchNames != "" {
			draftInput.CustomAttributes = append(draftInput.CustomAttributes,
				shopify.AttributeInput{Key: "hold_until_batch", Value: batchNames},
			)
		}
		draftInput.Note = formatShipTogetherHoldNote(batchNames)
	}

	country := req.Country
	if country == "" {
		country = "US"
	}

	shippingAddr := &shopify.AddressInput{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Company:   req.Company,
		Address1:  req.Address1,
		City:      req.City,
		Province:  req.State,
		Zip:       req.Zip,
		Country:   country,
		Phone:     req.Phone,
	}
	draftInput.ShippingAddress = shippingAddr

	initReq := InitiateCheckoutRequest{
		FirstName:        req.FirstName,
		LastName:         req.LastName,
		Company:          req.Company,
		Address1:         req.Address1,
		City:             req.City,
		State:            req.State,
		Zip:              req.Zip,
		Country:          country,
		Phone:            req.Phone,
		Email:            req.Email,
		SameAsShipping:   req.SameAsShipping,
		BillingFirstName: req.BillingFirstName,
		BillingLastName:  req.BillingLastName,
		BillingCompany:   req.BillingCompany,
		BillingAddress1:  req.BillingAddress1,
		BillingCity:      req.BillingCity,
		BillingState:     req.BillingState,
		BillingZip:       req.BillingZip,
		BillingCountry:   req.BillingCountry,
		BillingPhone:     req.BillingPhone,
	}
	draftInput.BillingAddress = resolveBillingAddress(initReq, shippingAddr)

	if snap := ShippingAddressSnapshotFromRequest(initReq); snap != nil {
		if jsonVal, err := snap.JSON(); err != nil {
			slog.WarnContext(ctx, "failed to marshal manual order shipping address snapshot",
				slog.String("checkout_reference", checkoutRef),
				slog.Any("error", err))
		} else {
			draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
				Key: ShippingAddressNoteAttribute, Value: jsonVal,
			})
		}
	}

	ratesBase := ShippingRatesRequest{
		Name:     strings.TrimSpace(req.FirstName + " " + req.LastName),
		Phone:    req.Phone,
		Address1: req.Address1,
		City:     req.City,
		State:    req.State,
		Zip:      req.Zip,
		Country:  country,
	}

	if len(shipReady) > 0 {
		ratesReq := ratesBase
		ratesReq.Segment = "ship_ready"
		ratesReq.LineItems = toShippingRateLineItems(shipReady)
		rates, rateErr := s.GetShippingRates(ctx, nil, nil, ratesReq)
		if rateErr != nil {
			slog.WarnContext(ctx, "manual order ship ready shipping rate lookup failed",
				slog.String("checkout_reference", checkoutRef),
				slog.Any("error", rateErr))
		} else if matched := s.matchShippingRate(rates, req.ShippingMethod, warehouse.CodeEast); matched != nil {
			draftInput.ShippingLine = shopify.NewShippingLineInput(matched.Label, matched.Cost, "USD")
		}
	}

	if len(preOrder) > 0 {
		ratesReq := ratesBase
		ratesReq.Segment = "pre_order"
		ratesReq.Origin = req.Origin
		ratesReq.LineItems = toShippingRateLineItems(preOrder)
		rates, rateErr := s.GetShippingRates(ctx, nil, nil, ratesReq)
		if rateErr != nil {
			slog.WarnContext(ctx, "manual order pre-order shipping rate lookup failed",
				slog.String("checkout_reference", checkoutRef),
				slog.Any("error", rateErr))
		} else {
			preOrderOrigin := warehouse.CodeEast
			if s.warehouseResolver != nil {
				preOrderOrigin = s.warehouseResolver.ResolveOrigin("pre_order", req.Origin)
			}
			if matched := s.matchShippingRate(rates, req.ShippingMethod, preOrderOrigin); matched != nil {
				draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
					Key: "preorder_shipping_estimate", Value: matched.Cost,
				})
			}
		}
	}

	res, err := s.shopifyCli.CreateDraftOrder(ctx, draftInput)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to create shopify draft order for manual order",
			slog.Any("error", err),
			slog.String("checkout_reference", checkoutRef))
		return nil, fmt.Errorf("failed to create shopify draft order: %w", err)
	}

	invoiceSent := true
	if sendErr := s.shopifyCli.SendDraftOrderInvoice(ctx, res.ID, &shopify.DraftOrderInvoiceEmailInput{
		To: req.Email,
	}); sendErr != nil {
		invoiceSent = false
		slog.ErrorContext(ctx, "Failed to send shopify draft order invoice email",
			slog.Any("error", sendErr),
			slog.String("checkout_reference", checkoutRef),
			slog.String("draft_order_id", res.ID))
	}

	return &ManualOrderResponse{
		InvoiceURL:        res.InvoiceUrl,
		CheckoutReference: checkoutRef,
		DraftOrderID:      res.ID,
		InvoiceEmailSent:  invoiceSent,
	}, nil
}

func toShippingRateLineItems(items []cart.CartItem) []ShippingRateLineItem {
	out := make([]ShippingRateLineItem, 0, len(items))
	for _, item := range items {
		out = append(out, ShippingRateLineItem{
			VariantID: item.VariantID,
			Quantity:  item.Quantity,
		})
	}
	return out
}

func (s *service) splitManualLineItems(ctx context.Context, lineItems []ManualOrderLineItem) (shipReady, preOrder []cart.CartItem, err error) {
	for _, li := range lineItems {
		variant, err := s.productService.GetVariantByID(ctx, li.VariantID)
		if err != nil {
			return nil, nil, apierror.New(404, "not_found", "Variant not found: "+li.VariantID)
		}
		if variant.FulfillmentType == product.FulfillmentTypeInactive {
			return nil, nil, apierror.New(422, "inactive_variant", "Variant is inactive: "+li.VariantID)
		}

		unitPrice := 0.0
		fmt.Sscanf(variant.WSPrice, "%f", &unitPrice)
		if unitPrice <= 0 {
			fmt.Sscanf(variant.RetailPrice, "%f", &unitPrice)
		}

		base := cart.CartItem{
			VariantID:         variant.ID,
			Title:             variant.Title,
			ImageSrc:          variant.ImageSrc,
			InventoryQuantity: variant.InventoryQuantity,
			UnitPrice:         fmt.Sprintf("%.2f", unitPrice),
			RetailPrice:       variant.RetailPrice,
			Weight:            variant.WeightKg,
			WeightUnit:        "KILOGRAMS",
			Length:            variant.LengthCm,
			Width:             variant.WidthCm,
			Height:            variant.HeightCm,
		}

		var shipQty, preQty int
		if variant.FulfillmentType == product.FulfillmentTypePreOrder {
			preQty = li.Quantity
		} else if li.Quantity <= variant.InventoryQuantity {
			shipQty = li.Quantity
		} else {
			shipQty = variant.InventoryQuantity
			preQty = li.Quantity - variant.InventoryQuantity
		}

		if shipQty > 0 {
			item := base
			item.Quantity = shipQty
			item.Subtotal = fmt.Sprintf("%.2f", unitPrice*float64(shipQty))
			shipReady = append(shipReady, item)
		}
		if preQty > 0 {
			item := base
			item.Quantity = preQty
			subtotal := unitPrice * float64(preQty)
			item.Subtotal = fmt.Sprintf("%.2f", subtotal)
			item.DepositAmount = fmt.Sprintf("%.2f", subtotal*0.5)
			item.BalanceDue = fmt.Sprintf("%.2f", subtotal*0.5)
			preOrder = append(preOrder, item)
		}
	}
	return shipReady, preOrder, nil
}
