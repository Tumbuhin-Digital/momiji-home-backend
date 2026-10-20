package order

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/warehouse"
)

const (
	preOrderStepPlaced             = 1
	preOrderStepShippingConfigured = 2
	preOrderStepSecondPayment      = 3
	preOrderStepShipped            = 4
	preOrderStepDelivered          = 5
)

func (s *service) zipLookup(ctx context.Context, zip string) (string, bool) {
	return s.store.GetUSZipStateAbbr(ctx, zip)
}

func (s *service) getPreOrderItems(o *Order) []OrderItem {
	var items []OrderItem
	for _, it := range o.Items {
		if it.Type == "pre_order" {
			items = append(items, it)
		}
	}
	return items
}

func ensurePackingQuantities(packing []PackingItemDTO, groupItems []OrderItem) []PackingItemDTO {
	qtyByID := make(map[string]int, len(groupItems))
	for _, it := range groupItems {
		qtyByID[it.ID] = it.Quantity
	}
	out := make([]PackingItemDTO, len(packing))
	copy(out, packing)
	for i := range out {
		sliceQty, ok := qtyByID[out[i].LineItemID]
		if !ok {
			continue
		}
		// Group slice qty is authoritative — clients/legacy packing may still send full line qty.
		out[i].Quantity = sliceQty
		if !out[i].IsNested && out[i].BoxCount > sliceQty {
			out[i].BoxCount = sliceQty
		}
	}
	return out
}

func (s *service) CalculatePreorderShipping(ctx context.Context, userID, orderID string, req CalculatePreorderShippingRequest) (*CalculatePreorderShippingResponse, error) {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}

	batchID := normalizeBatchIDPtr(req.BatchID)
	groupItems, err := s.resolveGroupSlices(ctx, o, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	groupItems, err = s.appendShipReadyForCombinedShipping(ctx, o, batchID, groupItems)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if len(groupItems) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No pre-order items for this fulfillment group")
	}

	packing := mergeDefaultPackingForMissingItems(req.Packing, groupItems)
	packing = ensurePackingQuantities(packing, groupItems)
	if err := ValidatePacking(packing, groupItems); err != nil {
		return nil, err
	}

	itemMap := make(map[string]OrderItem)
	var variantIDs []string
	for _, it := range groupItems {
		itemMap[it.ID] = it
		variantIDs = append(variantIDs, it.ShopifyVariantID)
	}

	dims, err := s.store.GetVariantDimensions(ctx, variantIDs)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	units, err := BuildPackableUnits(orderID, packing, itemMap, dims)
	if err != nil {
		return nil, err
	}
	packages, pkgErr := shipping.BuildPackages(ctx, units)
	if pkgErr != nil {
		return nil, shipping.ToAPIError(pkgErr)
	}
	if len(packages) == 0 {
		return nil, shipping.ToAPIError(shipping.PackageEmpty())
	}

	if o.ShippingAddress == nil {
		return nil, shipping.ToAPIError(shipping.AddressIncomplete([]string{"address", "city", "state", "zip", "country", "name", "phone"}))
	}

	addr := o.ShippingAddress
	firstName, lastName, phone := "", "", ""
	if addr.FirstName != nil {
		firstName = *addr.FirstName
	}
	if addr.LastName != nil {
		lastName = *addr.LastName
	}
	if addr.Phone != nil {
		phone = *addr.Phone
	}

	existingShipment, err := s.store.GetPreorderShipmentByBatch(ctx, orderID, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if existingShipment != nil && existingShipment.InvoicePaidAt != nil {
		return nil, errSecondPaymentAlreadyPaid
	}
	originCode := resolveWarehouseOrigin(existingShipment)
	// New batch groups should inherit warehouse from the order-level/legacy shipment.
	if existingShipment == nil {
		if legacy, lerr := s.store.GetPreorderShipment(ctx, orderID); lerr == nil && legacy != nil && legacy.WarehouseOrigin != "" {
			originCode = warehouse.NormalizeCode(legacy.WarehouseOrigin)
		}
	}
	origin, err := s.warehouseResolver.GetOrigin(ctx, originCode)
	if err != nil {
		return nil, shipping.ToAPIError(shipping.WarehouseOrigin("Failed to resolve warehouse origin"))
	}

	amount, currency, err := shipping.CalculateGroundRate(
		ctx,
		s.shipstationClient,
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
			Name:     strings.TrimSpace(firstName + " " + lastName),
			Phone:    phone,
			Address1: addr.Address1,
			City:     addr.City,
			State:    addr.Province,
			Zip:      addr.Zip,
			Country:  addr.Country,
		},
		packages,
		s.zipLookup,
	)
	if err != nil {
		slog.ErrorContext(ctx, "preorder calculate shipping rate failed",
			"order_id", orderID,
			"batch_id", batchID,
			"package_count", len(packages),
			"error", err,
		)
		return nil, shipping.ToAPIError(shipping.ClassifyCarrierFailure(err, shipping.MissingDimensionLabels(units)))
	}

	totalBoxes, totalWeight := PackingTotals(units)
	baseCost, bufferAmount, liveEstimate := shipping.ApplyShippingBuffer(amount)
	dbPacking := PackingToDBItems(packing)
	now := time.Now().UTC()

	shipment := &PreorderShipment{
		OrderID:          orderID,
		BatchID:          batchID,
		TotalBoxes:       totalBoxes,
		TotalWeightLb:    &totalWeight,
		WarehouseOrigin:  originCode,
		RateCalculatedAt: &now,
	}
	if existingShipment != nil {
		if existingShipment.EstimatedShipping != nil {
			// Preserve checkout estimate; live rate is returned in the response only.
		} else {
			shipment.EstimatedShipping = &liveEstimate
		}
		if existingShipment.WarehouseOrigin != "" {
			shipment.WarehouseOrigin = existingShipment.WarehouseOrigin
		}
	} else {
		shipment.EstimatedShipping = &liveEstimate
	}

	if err := s.store.UpsertPreorderShipment(ctx, shipment, dbPacking); err != nil {
		return nil, apierror.ErrInternal
	}

	groundCode := origin.GroundServiceCode
	if groundCode == "" {
		groundCode = "ups_ground"
	}

	return &CalculatePreorderShippingResponse{
		EstimatedShipping: fmt.Sprintf("%.2f", liveEstimate),
		BaseCost:          fmt.Sprintf("%.2f", baseCost),
		BufferAmount:      fmt.Sprintf("%.2f", bufferAmount),
		TotalBoxes:        totalBoxes,
		TotalWeightLb:     fmt.Sprintf("%.2f", totalWeight),
		Packing:           packing,
		ServiceCode:       groundCode,
		Currency:          currency,
		BatchID:           batchID,
		RateCalculatedAt:  now.Format(time.RFC3339),
	}, nil
}

func (s *service) UpdatePreorderShipping(ctx context.Context, userID, orderID string, req UpdatePreorderShippingRequest) (*PreorderShipmentDTO, error) {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}

	batchID := normalizeBatchIDPtr(req.BatchID)
	groupItems, err := s.resolveGroupSlices(ctx, o, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	// The settlement invoice bills only the pre-order slices; ship-ready items join the
	// group below for packing and weight alone.
	invoiceItems := groupItems
	groupItems, err = s.appendShipReadyForCombinedShipping(ctx, o, batchID, groupItems)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if len(groupItems) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No pre-order items for this fulfillment group")
	}

	shipment, err := s.store.GetPreorderShipmentByBatch(ctx, orderID, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if shipment != nil && shipment.InvoicePaidAt != nil {
		return nil, errSecondPaymentAlreadyPaid
	}

	// Final price can be set without a carrier/checkout estimate (LTL freight,
	// or when ShipStation cannot rate the shipment). Packing is required when
	// creating the shipment row for the first time.
	if shipment == nil && len(req.Packing) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Packing plan is required before setting final shipping price")
	}

	var packingShipment *PreorderShipment
	var packingItems []PreorderPackingItem
	if len(req.Packing) > 0 {
		packing := mergeDefaultPackingForMissingItems(req.Packing, groupItems)
		packing = ensurePackingQuantities(packing, groupItems)
		if err := ValidatePacking(packing, groupItems); err != nil {
			return nil, err
		}

		itemMap := make(map[string]OrderItem)
		var variantIDs []string
		for _, it := range groupItems {
			itemMap[it.ID] = it
			variantIDs = append(variantIDs, it.ShopifyVariantID)
		}

		dims, dimErr := s.store.GetVariantDimensions(ctx, variantIDs)
		if dimErr != nil {
			return nil, apierror.ErrInternal
		}

		units, packErr := BuildPackableUnits(orderID, packing, itemMap, dims)
		if packErr != nil {
			return nil, packErr
		}
		built, buildErr := shipping.BuildPackages(ctx, units)
		if buildErr != nil {
			return nil, shipping.ToAPIError(buildErr)
		}
		if len(built) == 0 {
			return nil, shipping.ToAPIError(shipping.PackageEmpty())
		}

		totalBoxes, totalWeight := PackingTotals(units)
		originCode := resolveWarehouseOrigin(shipment)
		if shipment == nil {
			if legacy, lerr := s.store.GetPreorderShipment(ctx, orderID); lerr == nil && legacy != nil && legacy.WarehouseOrigin != "" {
				originCode = warehouse.NormalizeCode(legacy.WarehouseOrigin)
			}
		}
		packingShipment = &PreorderShipment{
			OrderID:         orderID,
			BatchID:         batchID,
			TotalBoxes:      totalBoxes,
			TotalWeightLb:   &totalWeight,
			WarehouseOrigin: originCode,
		}
		if shipment != nil {
			packingShipment.EstimatedShipping = shipment.EstimatedShipping
			if shipment.WarehouseOrigin != "" {
				packingShipment.WarehouseOrigin = shipment.WarehouseOrigin
			}
		}
		packingItems = PackingToDBItems(packing)
	}

	// Once the payment link exists, the price lives on the Shopify invoice too. Re-price
	// that invoice after the request is validated but before anything is saved, so a
	// Shopify failure leaves both the invoice and our record as they were instead of
	// saving a price the customer is not being charged.
	invoiced := shipment != nil && shipment.InvoiceSentAt != nil
	prepaidApplied := 0.0
	if invoiced {
		prepaidApplied, err = s.repriceSecondPaymentInvoice(ctx, o, batchID, invoiceItems, shipment, req.FinalShippingPrice, req.ShippingNotes)
		if err != nil {
			return nil, err
		}
	}

	if packingShipment != nil {
		if err := s.store.UpsertPreorderShipment(ctx, packingShipment, packingItems); err != nil {
			return nil, apierror.ErrInternal
		}
		if shipment == nil {
			shipment = packingShipment
		} else {
			shipment.ID = packingShipment.ID
		}
	}

	if invoiced {
		if err := s.store.UpdateInvoicedPreorderShipping(ctx, shipment.ID, req.FinalShippingPrice, req.ShippingNotes, prepaidApplied); err != nil {
			// Shopify already holds the new price; say so loudly so the two can be reconciled.
			slog.ErrorContext(ctx, "shopify invoice re-priced but shipping price not saved",
				"order_id", orderID,
				"shipment_id", shipment.ID,
				"final_shipping_price", req.FinalShippingPrice,
				"prepaid_applied", prepaidApplied,
				"error", err,
			)
			if errors.Is(err, ErrPreorderShipmentPaid) {
				return nil, errSecondPaymentAlreadyPaid
			}
			return nil, apierror.ErrInternal
		}
	} else {
		credit := 0.0
		if err := s.store.UpdatePreorderShippingByShipmentID(ctx, shipment.ID, req.FinalShippingPrice, req.ShippingNotes, credit); err != nil {
			return nil, apierror.ErrInternal
		}
	}

	// Advance this group's items toward shipping-configured as soon as THIS shipment has a final price.
	// Per-group second payment no longer waits for all groups.
	for _, it := range groupItems {
		if it.FulfillmentStep < preOrderStepShippingConfigured {
			if err := s.store.UpdateOrderItemStep(ctx, it.ID, preOrderStepShippingConfigured); err != nil {
				return nil, apierror.ErrInternal
			}
		}
	}

	updated, err := s.store.GetPreorderShipmentByBatch(ctx, orderID, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	dto := s.toPreorderShipmentDTO(updated)
	return &dto, nil
}

var errSecondPaymentAlreadyPaid = apierror.New(http.StatusConflict, "invalid_transition", "This group's second payment has already been paid; shipping can no longer be changed")

// repriceSecondPaymentInvoice rebuilds an unpaid group invoice at the new shipping price and
// writes it onto the same Shopify draft, so the payment link stays the same and shows the
// new total. Nothing is emailed; the admin resends the invoice when they choose to.
// It returns the prepayment share the re-priced invoice deducts, for the caller to save.
func (s *service) repriceSecondPaymentInvoice(
	ctx context.Context,
	o *Order,
	batchID *string,
	invoiceItems []OrderItem,
	shipment *PreorderShipment,
	finalPrice float64,
	notes string,
) (float64, error) {
	_, draftOrderID := shipmentInvoiceRefs(shipment)
	if draftOrderID == "" {
		// Invoices sent before per-group drafts were tracked have no draft to update.
		return 0, apierror.New(http.StatusConflict, "invalid_transition", "This group's invoice was created without a Shopify draft and cannot be re-priced")
	}

	allShipments, err := s.store.GetPreorderShipments(ctx, o.ID)
	if err != nil {
		return 0, apierror.ErrInternal
	}
	prepaidApplied := RepricedPrepaidApplied(allShipments, shipment.ID, finalPrice)

	repriced := *shipment
	repriced.FinalShippingPrice = &finalPrice
	repriced.ShippingNotes = &notes
	plan, err := s.buildGroupSecondPaymentPlan(ctx, o, batchID, invoiceItems, &repriced, prepaidApplied)
	if err != nil {
		return 0, err
	}

	if _, err := s.preorderService.UpdateGroupSecondPaymentInvoice(ctx, draftOrderID, plan.opts); err != nil {
		return 0, err
	}
	return prepaidApplied, nil
}

func (s *service) allPreorderGroupsHaveFinalShipping(ctx context.Context, o *Order, shipments []PreorderShipment) (bool, error) {
	// Build expected group keys from allocations.
	preItems := s.getPreOrderItems(o)
	if len(preItems) == 0 {
		return true, nil
	}
	lineIDs := make([]string, 0, len(preItems))
	for _, it := range preItems {
		lineIDs = append(lineIDs, it.ID)
	}

	expected := make(map[string]bool) // batchID or ""
	allocatedQty := make(map[string]int)
	if s.batchService != nil {
		allocs, err := s.batchService.GetCommittedAllocationsByOrderLineItemIDs(ctx, lineIDs)
		if err != nil {
			return false, err
		}
		for _, a := range allocs {
			if a.OrderLineItemID == nil {
				continue
			}
			expected[a.BatchID] = true
			allocatedQty[*a.OrderLineItemID] += a.Quantity
		}
	}
	for _, it := range preItems {
		if it.Quantity-allocatedQty[it.ID] > 0 {
			expected[""] = true
		}
	}
	// Fallback: no allocations → single unbatched group
	if len(expected) == 0 {
		expected[""] = true
	}

	configured := make(map[string]bool)
	for _, sh := range shipments {
		if sh.FinalShippingPrice == nil {
			continue
		}
		key := ""
		if sh.BatchID != nil {
			key = *sh.BatchID
		}
		configured[key] = true
	}

	for key := range expected {
		if !configured[key] {
			return false, nil
		}
	}
	return true, nil
}

type groupSecondPaymentPlan struct {
	shipment       *PreorderShipment
	groupItems     []OrderItem
	opts           preorder.GroupInvoiceOptions
	existingURL    string
	existingDraft  string
	prepaidApplied float64
}

func (s *service) RequestSecondPayment(ctx context.Context, userID, orderID string, req RequestSecondPaymentRequest) (*RequestSecondPaymentResponse, error) {
	plan, err := s.prepareGroupSecondPayment(ctx, userID, orderID, req)
	if err != nil {
		return nil, err
	}
	if plan.existingURL != "" {
		return &RequestSecondPaymentResponse{
			InvoiceURL:   plan.existingURL,
			DraftOrderID: plan.existingDraft,
		}, nil
	}

	result, err := s.preorderService.CreateGroupSecondPaymentInvoice(ctx, plan.opts)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if err := s.store.MarkPreorderShipmentInvoiceSent(ctx, plan.shipment.ID, result.DraftOrderID, result.InvoiceURL, now, plan.prepaidApplied); err != nil {
		return nil, apierror.ErrInternal
	}

	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil || o == nil {
		return nil, apierror.ErrInternal
	}
	if err := s.store.UpdateOrderStatus(ctx, orderID, "on_progress", o.FinancialStatus, "waiting"); err != nil {
		return nil, apierror.ErrInternal
	}
	for _, it := range plan.groupItems {
		if err := s.store.UpdateItemStatusByID(ctx, it.ID, "waiting_payment"); err != nil {
			return nil, apierror.ErrInternal
		}
		if it.FulfillmentStep < preOrderStepSecondPayment {
			if err := s.store.UpdateOrderItemStep(ctx, it.ID, preOrderStepSecondPayment); err != nil {
				return nil, apierror.ErrInternal
			}
		}
	}

	return &RequestSecondPaymentResponse{
		InvoiceURL:   result.InvoiceURL,
		DraftOrderID: result.DraftOrderID,
	}, nil
}

func (s *service) ResendSecondPaymentInvoice(ctx context.Context, userID, orderID string, req RequestSecondPaymentRequest) error {
	resp, err := s.RequestSecondPayment(ctx, userID, orderID, req)
	if err != nil {
		return err
	}
	plan, err := s.prepareGroupSecondPayment(ctx, userID, orderID, req)
	if err != nil {
		return err
	}
	return s.preorderService.SendGroupSecondPaymentInvoiceEmail(ctx, plan.opts, resp.InvoiceURL)
}

func (s *service) prepareGroupSecondPayment(ctx context.Context, userID, orderID string, req RequestSecondPaymentRequest) (*groupSecondPaymentPlan, error) {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}

	batchID := normalizeBatchIDPtr(req.BatchID)
	groupItems, err := s.resolveGroupSlices(ctx, o, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if len(groupItems) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No pre-order items for this fulfillment group")
	}

	shipment, err := s.store.GetPreorderShipmentByBatch(ctx, orderID, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if shipment == nil || shipment.FinalShippingPrice == nil {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Final shipping price must be set for this group before requesting payment")
	}

	existingURL, _ := shipmentInvoiceRefs(shipment)
	if existingURL == "" && shipment.InvoiceSentAt != nil {
		return nil, apierror.New(http.StatusConflict, "invalid_transition", "Second payment invoice already sent for this group")
	}

	// The checkout prepayment is collected once for the whole order but settlement runs per
	// group, so ask the allocator how much of the pool this group may still use rather than
	// reading the row's own PrepaidShipping (which is 0 on batch rows).
	allShipments, err := s.store.GetPreorderShipments(ctx, orderID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	prepaidApplied := AllocatePrepaidShipping(allShipments)[shipment.ID]

	return s.buildGroupSecondPaymentPlan(ctx, o, batchID, groupItems, shipment, prepaidApplied)
}

func shipmentInvoiceRefs(shipment *PreorderShipment) (invoiceURL, draftOrderID string) {
	if shipment.InvoiceURL != nil {
		invoiceURL = strings.TrimSpace(*shipment.InvoiceURL)
	}
	if shipment.ShopifyDraftOrderID != nil {
		draftOrderID = strings.TrimSpace(*shipment.ShopifyDraftOrderID)
	}
	return invoiceURL, draftOrderID
}

// buildGroupSecondPaymentPlan prices one group's settlement invoice from the given shipment
// and prepayment share. It reads but never writes, so a re-price can build the new invoice
// and push it to Shopify before anything is saved.
func (s *service) buildGroupSecondPaymentPlan(
	ctx context.Context,
	o *Order,
	batchID *string,
	groupItems []OrderItem,
	shipment *PreorderShipment,
	prepaidApplied float64,
) (*groupSecondPaymentPlan, error) {
	orderID := o.ID
	existingURL, existingDraft := shipmentInvoiceRefs(shipment)

	if o.ShippingAddress == nil {
		return nil, shipping.ToAPIError(shipping.AddressIncomplete([]string{"address", "city", "state", "zip", "country", "name", "phone"}))
	}

	customerEmail := ""
	customerName := "Customer"
	if o.Customer != nil {
		if o.Customer.Email != "" {
			customerEmail = o.Customer.Email
		}
		if o.Customer.FirstName != nil && *o.Customer.FirstName != "" {
			customerName = *o.Customer.FirstName
		}
	}

	// SKUs are display-only on the invoice lines; a failed lookup just leaves them blank.
	variantIDs := make([]string, 0, len(groupItems))
	for _, it := range groupItems {
		variantIDs = append(variantIDs, it.ShopifyVariantID)
	}
	variantInfo, err := s.store.GetVariantDimensions(ctx, variantIDs)
	if err != nil {
		slog.WarnContext(ctx, "preorder second payment: variant SKU lookup failed",
			"order_id", orderID,
			"error", err,
		)
	}

	invoiceLines := make([]preorder.GroupInvoiceLine, 0, len(groupItems))
	for _, it := range groupItems {
		if it.BalanceDue == nil || *it.BalanceDue <= 0 || it.Quantity < 1 {
			continue
		}
		fullQty := it.Quantity
		// resolveGroupSlices overwrites Quantity with slice qty; recover full line qty from order.
		fullLineQty := it.Quantity
		for _, raw := range o.Items {
			if raw.ID == it.ID {
				fullLineQty = raw.Quantity
				break
			}
		}
		if fullLineQty < 1 {
			fullLineQty = fullQty
		}
		prorated := *it.BalanceDue * float64(fullQty) / float64(fullLineQty)
		prorated = math.Round(prorated*100) / 100

		title := ""
		if it.Title != nil {
			title = *it.Title
		}
		invoiceLines = append(invoiceLines, preorder.GroupInvoiceLine{
			Title:           title,
			SKU:             variantInfo[it.ShopifyVariantID].SKU,
			Amount:          prorated,
			OrderLineItemID: it.ID,
			Quantity:        fullQty,
			ShipmentID:      shipment.ID,
			OrderID:         orderID,
		})

		if customerEmail == "" {
			st, serr := s.preorderStore.GetSettlementByOrderLineItemID(ctx, it.ID)
			if serr == nil && st != nil {
				customerEmail = st.CustomerEmail
				if st.CustomerName != "" {
					customerName = st.CustomerName
				}
			}
		}
	}

	remainingShipping := math.Round((*shipment.FinalShippingPrice-prepaidApplied)*100) / 100
	if remainingShipping < 0 {
		remainingShipping = 0
	}

	if len(invoiceLines) == 0 && remainingShipping <= 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Nothing to invoice for this group")
	}

	shippingMethod := ""
	if o.ShippingMethod != nil {
		shippingMethod = *o.ShippingMethod
	}
	notes := ""
	if shipment.ShippingNotes != nil {
		notes = strings.TrimSpace(*shipment.ShippingNotes)
	}

	batchName := ""
	itemCount := 0
	for _, it := range groupItems {
		if it.Quantity > 0 {
			itemCount += it.Quantity
		}
	}
	if batchID != nil && *batchID != "" && s.batchService != nil {
		batches, berr := s.batchService.GetBatchesByIDs(ctx, []string{*batchID})
		if berr == nil {
			for _, b := range batches {
				if b.ID == *batchID {
					batchName = strings.TrimSpace(b.Name)
					break
				}
			}
		}
	}

	return &groupSecondPaymentPlan{
		shipment:   shipment,
		groupItems: groupItems,
		opts: preorder.GroupInvoiceOptions{
			CustomerEmail:      customerEmail,
			CustomerName:       customerName,
			OrderID:            orderID,
			DepositOrderNumber: shopifyOrderNumber(o.OrderNumber),
			ShipmentID:         shipment.ID,
			BatchName:          batchName,
			ItemCount:          itemCount,
			Lines:              invoiceLines,
			ShippingTitle:      shippingMethod,
			ShippingPrice:      remainingShipping,
			ShippingPrepaid:    prepaidApplied,
			ShippingNotes:      notes,
			ShippingAddress:    orderAddressToShopify(o.ShippingAddress),
			BillingAddress:     orderAddressToShopify(billingAddressOrShipping(o)),
		},
		existingURL:    existingURL,
		existingDraft:  existingDraft,
		prepaidApplied: prepaidApplied,
	}, nil
}

// shopifyOrderNumber returns the Shopify order number ("1323") behind a local order number
// ("ORD-1323"), or "" for older orders whose number was not taken from Shopify.
func shopifyOrderNumber(localOrderNumber string) string {
	n := strings.TrimPrefix(strings.TrimSpace(localOrderNumber), "ORD-")
	if _, err := strconv.ParseUint(n, 10, 64); err != nil {
		return ""
	}
	return n
}

func billingAddressOrShipping(o *Order) *customer.Address {
	if o == nil {
		return nil
	}
	if o.BillingAddress != nil && strings.TrimSpace(o.BillingAddress.Address1) != "" {
		return o.BillingAddress
	}
	return o.ShippingAddress
}

func orderAddressToShopify(addr *customer.Address) *shopify.AddressInput {
	if addr == nil {
		return nil
	}

	firstName, lastName, phone, company := "", "", "", ""
	if addr.FirstName != nil {
		firstName = *addr.FirstName
	}
	if addr.LastName != nil {
		lastName = *addr.LastName
	}
	if addr.Phone != nil {
		phone = *addr.Phone
	}
	if addr.Company != nil {
		company = *addr.Company
	}

	return &shopify.AddressInput{
		FirstName: firstName,
		LastName:  lastName,
		Company:   company,
		Address1:  addr.Address1,
		City:      addr.City,
		Province:  addr.Province,
		Zip:       addr.Zip,
		Country:   addr.Country,
		Phone:     phone,
	}
}

// toPreorderShipmentDTOWithAllocation reports the share of the order's checkout
// prepayment this group may actually deduct, not the raw pool value on the row — the
// pool sits on the checkout-created row and is shared across batch groups.
func (s *service) toPreorderShipmentDTOWithAllocation(
	shipment *PreorderShipment,
	shipments []PreorderShipment,
) PreorderShipmentDTO {
	return s.toPreorderShipmentDTOWithPrepaid(shipment, AllocatePrepaidShipping(shipments), PrepaidShippingLimits(shipments))
}

func (s *service) toPreorderShipmentDTOWithPrepaid(
	shipment *PreorderShipment,
	allocation map[string]float64,
	limits map[string]float64,
) PreorderShipmentDTO {
	dto := s.toPreorderShipmentDTO(shipment)
	if shipment == nil {
		return dto
	}
	// Always emit a figure, including "0.00". Omitting it would make "this group gets no
	// credit" indistinguishable from "unknown", and clients that fall back on a missing
	// value would show another group's share.
	v := fmt.Sprintf("%.2f", allocation[shipment.ID])
	dto.PrepaidShipping = &v
	limit := fmt.Sprintf("%.2f", limits[shipment.ID])
	dto.PrepaidShippingLimit = &limit
	return dto
}

func (s *service) toPreorderShipmentDTO(shipment *PreorderShipment) PreorderShipmentDTO {
	if shipment == nil {
		return PreorderShipmentDTO{}
	}
	dto := PreorderShipmentDTO{
		ID:              shipment.ID,
		BatchID:         shipment.BatchID,
		TotalBoxes:      shipment.TotalBoxes,
		WarehouseOrigin: shipment.WarehouseOrigin,
	}
	if shipment.EstimatedShipping != nil {
		v := fmt.Sprintf("%.2f", *shipment.EstimatedShipping)
		dto.EstimatedShipping = &v
	}
	if shipment.FinalShippingPrice != nil {
		v := fmt.Sprintf("%.2f", *shipment.FinalShippingPrice)
		dto.FinalShippingPrice = &v
	}
	if shipment.PrepaidShipping > 0 {
		v := fmt.Sprintf("%.2f", shipment.PrepaidShipping)
		dto.PrepaidShipping = &v
	}
	if shipment.ShippingNotes != nil {
		dto.ShippingNotes = shipment.ShippingNotes
	}
	if shipment.CreditAmount > 0 {
		v := fmt.Sprintf("%.2f", shipment.CreditAmount)
		dto.CreditAmount = &v
	}
	if shipment.TotalWeightLb != nil {
		v := fmt.Sprintf("%.2f", *shipment.TotalWeightLb)
		dto.TotalWeightLb = &v
	}
	if shipment.RateCalculatedAt != nil {
		v := shipment.RateCalculatedAt.UTC().Format(time.RFC3339)
		dto.RateCalculatedAt = &v
	}
	if shipment.InvoiceSentAt != nil {
		v := shipment.InvoiceSentAt.Format(time.RFC3339)
		dto.InvoiceSentAt = &v
	}
	if shipment.InvoiceURL != nil {
		dto.InvoiceURL = shipment.InvoiceURL
	}
	if shipment.InvoicePaidAt != nil {
		v := shipment.InvoicePaidAt.Format(time.RFC3339)
		dto.InvoicePaidAt = &v
	}
	if shipment.ShopifyDraftOrderID != nil {
		dto.ShopifyDraftOrderID = shipment.ShopifyDraftOrderID
	}
	for _, p := range shipment.PackingItems {
		dto.Packing = append(dto.Packing, PackingItemDTO{
			LineItemID: p.OrderLineItemID,
			Quantity:   p.Quantity,
			BoxCount:   p.BoxCount,
			IsNested:   p.IsNested,
		})
	}
	return dto
}

func (s *service) enrichOrderItemDetails(ctx context.Context, items []OrderItem) map[string]VariantDimensions {
	var variantIDs []string
	for _, it := range items {
		variantIDs = append(variantIDs, it.ShopifyVariantID)
	}
	dims, _ := s.store.GetVariantDimensions(ctx, variantIDs)
	return dims
}
