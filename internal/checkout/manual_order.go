package checkout

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
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
			return nil, rateErr
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
			return nil, rateErr
		} else {
			preOrderOrigin := warehouse.CodeEast
			if s.warehouseResolver != nil {
				preOrderOrigin = s.warehouseResolver.ResolveOrigin("pre_order", req.Origin)
			}
			if matched := s.matchShippingRate(rates, req.ShippingMethod, preOrderOrigin); matched != nil {
				draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
					Key: "preorder_shipping_estimate", Value: matched.Cost,
				})
				estimate, parseErr := strconv.ParseFloat(matched.Cost, 64)
				if parseErr != nil {
					slog.WarnContext(ctx, "unparseable manual order pre-order shipping estimate; skipping upfront half",
						slog.String("checkout_reference", checkoutRef),
						slog.String("value", matched.Cost))
				} else if upfront, _ := shipping.SplitHalf(estimate); upfront > 0 {
					draftInput.LineItems = append(draftInput.LineItems, buildPreOrderShippingDepositLine(upfront))
					draftInput.CustomAttributes = append(draftInput.CustomAttributes, shopify.AttributeInput{
						Key: PreOrderShippingPrepaidAttribute, Value: fmt.Sprintf("%.2f", upfront),
					})
				}
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

	return &ManualOrderResponse{
		InvoiceURL:        res.InvoiceUrl,
		CheckoutReference: checkoutRef,
		DraftOrderID:      res.ID,
		InvoiceEmailSent:  false,
	}, nil
}

func (s *service) SendManualOrderInvoice(ctx context.Context, req SendManualOrderInvoiceRequest) (*SendManualOrderInvoiceResponse, error) {
	draftID := strings.TrimSpace(req.DraftOrderID)
	to := strings.TrimSpace(req.Email)
	if draftID == "" || to == "" {
		return nil, apierror.New(400, "bad_request", "draft_order_id and email are required")
	}

	if _, err := s.loadEditableWebsiteDraft(ctx, draftID); err != nil {
		return nil, err
	}

	if err := s.shopifyCli.SendDraftOrderInvoice(ctx, draftID, &shopify.DraftOrderInvoiceEmailInput{
		To: to,
	}); err != nil {
		slog.ErrorContext(ctx, "Failed to send shopify draft order invoice email",
			slog.Any("error", err),
			slog.String("draft_order_id", draftID))
		return nil, apierror.New(502, "invoice_send_failed", "Failed to send Shopify invoice email")
	}

	return &SendManualOrderInvoiceResponse{InvoiceEmailSent: true}, nil
}

func (s *service) UpdateDraftOrderItems(ctx context.Context, req UpdateDraftOrderItemsRequest) (*UpdateDraftOrderItemsResponse, error) {
	draftID := strings.TrimSpace(req.DraftOrderID)
	if draftID == "" {
		return nil, apierror.New(400, "bad_request", "draft_order_id is required")
	}
	if len(req.LineItems) == 0 {
		return nil, apierror.New(400, "bad_request", "at least one line item is required")
	}

	draft, err := s.loadEditableWebsiteDraft(ctx, draftID)
	if err != nil {
		return nil, err
	}
	if draft.ShippingAddress == nil || strings.TrimSpace(draft.ShippingAddress.Zip) == "" {
		return nil, apierror.New(400, "bad_request", "draft is missing a shipping address")
	}

	shipReady, preOrder, err := s.splitManualLineItems(ctx, req.LineItems)
	if err != nil {
		return nil, err
	}
	if len(shipReady) == 0 && len(preOrder) == 0 {
		return nil, apierror.New(400, "bad_request", "no valid line items")
	}

	shipTogether := draftAttr(draft.CustomAttributes, "ship_together") == "true" &&
		len(shipReady) > 0 && len(preOrder) > 0
	shipReady, preOrder = applyShipTogetherSegments(shipReady, preOrder, shipTogether)

	shippingMethod := strings.TrimSpace(req.ShippingMethod)
	if len(preOrder) > 0 && shippingMethod == "" {
		return nil, apierror.New(400, "bad_request", "shipping_method is required when pre-order items are present")
	}

	for _, item := range append(shipReady, preOrder...) {
		if err := s.productService.ValidateVariantActive(ctx, item.VariantID); err != nil {
			return nil, err
		}
	}

	draftInput := shopify.DraftOrderInput{
		LineItems:        buildDraftLinesFromSegments(shipReady, preOrder),
		Email:            draft.Email,
		ShippingAddress:  draftAddressInput(draft.ShippingAddress),
		BillingAddress:   draftAddressInput(draft.BillingAddress),
		CustomAttributes: append([]shopify.AttributeInput{}, draft.CustomAttributes...),
		Note:             draft.Note,
	}

	origin := draftAttr(draft.CustomAttributes, "preorder_warehouse_origin")
	country := draft.ShippingAddress.Country
	if country == "" {
		country = "US"
	}
	ratesBase := ShippingRatesRequest{
		Name:     strings.TrimSpace(draft.ShippingAddress.FirstName + " " + draft.ShippingAddress.LastName),
		Phone:    draft.ShippingAddress.Phone,
		Address1: draft.ShippingAddress.Address1,
		City:     draft.ShippingAddress.City,
		State:    draft.ShippingAddress.Province,
		Zip:      draft.ShippingAddress.Zip,
		Country:  country,
	}

	if len(shipReady) > 0 {
		ratesReq := ratesBase
		ratesReq.Segment = "ship_ready"
		ratesReq.LineItems = toShippingRateLineItems(shipReady)
		rates, rateErr := s.GetShippingRates(ctx, nil, nil, ratesReq)
		if rateErr != nil {
			slog.WarnContext(ctx, "draft edit ship ready shipping rate lookup failed",
				slog.String("draft_order_id", draftID),
				slog.Any("error", rateErr))
			return nil, rateErr
		} else if matched := s.matchShippingRate(rates, shippingMethod, warehouse.CodeEast); matched != nil {
			draftInput.ShippingLine = shopify.NewShippingLineInput(matched.Label, matched.Cost, "USD")
		}
	}

	if len(preOrder) > 0 {
		ratesReq := ratesBase
		ratesReq.Segment = "pre_order"
		ratesReq.Origin = origin
		ratesReq.LineItems = toShippingRateLineItems(preOrder)
		rates, rateErr := s.GetShippingRates(ctx, nil, nil, ratesReq)
		if rateErr != nil {
			slog.WarnContext(ctx, "draft edit pre-order shipping rate lookup failed",
				slog.String("draft_order_id", draftID),
				slog.Any("error", rateErr))
			return nil, rateErr
		} else {
			preOrderOrigin := warehouse.CodeEast
			if s.warehouseResolver != nil {
				preOrderOrigin = s.warehouseResolver.ResolveOrigin("pre_order", origin)
			}
			if matched := s.matchShippingRate(rates, shippingMethod, preOrderOrigin); matched != nil {
				draftInput.CustomAttributes = upsertDraftAttr(draftInput.CustomAttributes, "preorder_shipping_estimate", matched.Cost)
				estimate, parseErr := strconv.ParseFloat(matched.Cost, 64)
				if parseErr != nil {
					slog.WarnContext(ctx, "unparseable draft edit pre-order shipping estimate; skipping upfront half",
						slog.String("draft_order_id", draftID),
						slog.String("value", matched.Cost))
					draftInput.CustomAttributes = removeDraftAttr(draftInput.CustomAttributes, PreOrderShippingPrepaidAttribute)
				} else if upfront, _ := shipping.SplitHalf(estimate); upfront > 0 {
					draftInput.LineItems = append(draftInput.LineItems, buildPreOrderShippingDepositLine(upfront))
					draftInput.CustomAttributes = upsertDraftAttr(draftInput.CustomAttributes, PreOrderShippingPrepaidAttribute, fmt.Sprintf("%.2f", upfront))
				} else {
					draftInput.CustomAttributes = removeDraftAttr(draftInput.CustomAttributes, PreOrderShippingPrepaidAttribute)
				}
			}
		}
	} else {
		draftInput.CustomAttributes = removeDraftAttr(draftInput.CustomAttributes, "preorder_shipping_estimate")
		draftInput.CustomAttributes = removeDraftAttr(draftInput.CustomAttributes, PreOrderShippingPrepaidAttribute)
	}
	if shippingMethod != "" {
		draftInput.CustomAttributes = upsertDraftAttr(draftInput.CustomAttributes, "preorder_shipping_method", shippingMethod)
	}

	res, err := s.shopifyCli.UpdateDraftOrder(ctx, draftID, draftInput, draftInput.ShippingLine == nil)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to update shopify draft order",
			slog.Any("error", err),
			slog.String("draft_order_id", draftID))
		return nil, apierror.New(502, "draft_update_failed", "Failed to update the Shopify draft order")
	}

	return &UpdateDraftOrderItemsResponse{
		DraftOrderID: res.ID,
		InvoiceURL:   res.InvoiceUrl,
	}, nil
}

func (s *service) loadEditableWebsiteDraft(ctx context.Context, draftID string) (*shopify.DraftOrderDetail, error) {
	draft, err := s.shopifyCli.GetDraftOrder(ctx, draftID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load shopify draft order",
			slog.String("draft_order_id", draftID),
			slog.Any("error", err))
		return nil, apierror.ErrInternal
	}
	if draft == nil || !draftHasWholesaleSource(draft) {
		return nil, apierror.ErrNotFound
	}
	if strings.EqualFold(draft.Status, "COMPLETED") {
		return nil, apierror.New(409, "draft_completed", "Completed draft orders cannot be changed")
	}
	return draft, nil
}

func draftHasWholesaleSource(draft *shopify.DraftOrderDetail) bool {
	for _, attr := range draft.CustomAttributes {
		if attr.Key == shopify.WholesaleSourceAttribute.Key && attr.Value == shopify.WholesaleSourceAttribute.Value {
			return true
		}
	}
	return false
}

func draftAttr(attrs []shopify.AttributeInput, key string) string {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value
		}
	}
	return ""
}

func upsertDraftAttr(attrs []shopify.AttributeInput, key, value string) []shopify.AttributeInput {
	for i := range attrs {
		if attrs[i].Key == key {
			attrs[i].Value = value
			return attrs
		}
	}
	return append(attrs, shopify.AttributeInput{Key: key, Value: value})
}

func removeDraftAttr(attrs []shopify.AttributeInput, key string) []shopify.AttributeInput {
	out := make([]shopify.AttributeInput, 0, len(attrs))
	for _, attr := range attrs {
		if attr.Key == key {
			continue
		}
		out = append(out, attr)
	}
	return out
}

func draftAddressInput(addr *shopify.DraftOrderAddress) *shopify.AddressInput {
	if addr == nil {
		return nil
	}
	return &shopify.AddressInput{
		FirstName: addr.FirstName,
		LastName:  addr.LastName,
		Company:   addr.Company,
		Address1:  addr.Address1,
		City:      addr.City,
		Province:  addr.Province,
		Zip:       addr.Zip,
		Country:   addr.Country,
		Phone:     addr.Phone,
	}
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
