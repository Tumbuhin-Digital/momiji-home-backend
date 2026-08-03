package order

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
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
	if len(groupItems) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No pre-order items for this fulfillment group")
	}

	packing := ensurePackingQuantities(req.Packing, groupItems)
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
	packages := shipping.BuildPackages(ctx, units)
	if len(packages) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No shippable boxes in packing plan")
	}

	if o.ShippingAddress == nil {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Order has no shipping address")
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
	originCode := resolveWarehouseOrigin(existingShipment)
	// New batch groups should inherit warehouse from the order-level/legacy shipment.
	if existingShipment == nil {
		if legacy, lerr := s.store.GetPreorderShipment(ctx, orderID); lerr == nil && legacy != nil && legacy.WarehouseOrigin != "" {
			originCode = warehouse.NormalizeCode(legacy.WarehouseOrigin)
		}
	}
	origin, err := s.warehouseResolver.GetOrigin(ctx, originCode)
	if err != nil {
		return nil, apierror.New(http.StatusInternalServerError, "shipping_rate_error", "Failed to resolve warehouse origin")
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
		return nil, apierror.New(http.StatusInternalServerError, "shipping_rate_error", fmt.Sprintf("Failed to fetch shipping rates from carriers: %v", err))
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
	if len(groupItems) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No pre-order items for this fulfillment group")
	}

	shipment, err := s.store.GetPreorderShipmentByBatch(ctx, orderID, batchID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	// Final price can be set without a carrier/checkout estimate (LTL freight,
	// or when ShipStation cannot rate the shipment). Packing is required when
	// creating the shipment row for the first time.
	if shipment == nil && len(req.Packing) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Packing plan is required before setting final shipping price")
	}

	if len(req.Packing) > 0 {
		packing := ensurePackingQuantities(req.Packing, groupItems)
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
		if len(shipping.BuildPackages(ctx, units)) == 0 {
			return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No shippable boxes in packing plan")
		}

		totalBoxes, totalWeight := PackingTotals(units)
		originCode := resolveWarehouseOrigin(shipment)
		if shipment == nil {
			if legacy, lerr := s.store.GetPreorderShipment(ctx, orderID); lerr == nil && legacy != nil && legacy.WarehouseOrigin != "" {
				originCode = warehouse.NormalizeCode(legacy.WarehouseOrigin)
			}
		}
		packingShipment := &PreorderShipment{
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
		if err := s.store.UpsertPreorderShipment(ctx, packingShipment, PackingToDBItems(packing)); err != nil {
			return nil, apierror.ErrInternal
		}
		if shipment == nil {
			shipment = packingShipment
		} else {
			shipment.ID = packingShipment.ID
		}
	}

	credit := 0.0
	if err := s.store.UpdatePreorderShippingByShipmentID(ctx, shipment.ID, req.FinalShippingPrice, req.ShippingNotes, credit); err != nil {
		return nil, apierror.ErrInternal
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

func (s *service) RequestSecondPayment(ctx context.Context, userID, orderID string, req RequestSecondPaymentRequest) error {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return apierror.ErrInternal
	}
	if o == nil {
		return apierror.ErrNotFound
	}

	batchID := normalizeBatchIDPtr(req.BatchID)
	groupItems, err := s.resolveGroupSlices(ctx, o, batchID)
	if err != nil {
		return apierror.ErrInternal
	}
	if len(groupItems) == 0 {
		return apierror.New(http.StatusBadRequest, "invalid_request", "No pre-order items for this fulfillment group")
	}

	shipment, err := s.store.GetPreorderShipmentByBatch(ctx, orderID, batchID)
	if err != nil {
		return apierror.ErrInternal
	}
	if shipment == nil || shipment.FinalShippingPrice == nil {
		return apierror.New(http.StatusBadRequest, "invalid_request", "Final shipping price must be set for this group before requesting payment")
	}
	if shipment.InvoiceSentAt != nil {
		return apierror.New(http.StatusConflict, "invalid_transition", "Second payment invoice already sent for this group")
	}

	if o.ShippingAddress == nil {
		return apierror.New(http.StatusBadRequest, "invalid_request", "Order has no shipping address")
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

	if len(invoiceLines) == 0 && *shipment.FinalShippingPrice <= 0 {
		return apierror.New(http.StatusBadRequest, "invalid_request", "Nothing to invoice for this group")
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

	result, err := s.preorderService.CreateGroupSecondPaymentInvoice(ctx, preorder.GroupInvoiceOptions{
		CustomerEmail:   customerEmail,
		CustomerName:    customerName,
		OrderID:         orderID,
		ShipmentID:      shipment.ID,
		BatchName:       batchName,
		ItemCount:       itemCount,
		Lines:           invoiceLines,
		ShippingTitle:   shippingMethod,
		ShippingPrice:   *shipment.FinalShippingPrice,
		ShippingNotes:   notes,
		ShippingAddress: orderAddressToShopify(o.ShippingAddress),
		BillingAddress:  orderAddressToShopify(billingAddressOrShipping(o)),
	})
	if err != nil {
		return err
	}

	now := time.Now()
	if err := s.store.MarkPreorderShipmentInvoiceSent(ctx, shipment.ID, result.DraftOrderID, result.InvoiceURL, now); err != nil {
		return apierror.ErrInternal
	}

	if err := s.store.UpdateOrderStatus(ctx, orderID, "on_progress", o.FinancialStatus, "waiting"); err != nil {
		return apierror.ErrInternal
	}
	for _, it := range groupItems {
		if err := s.store.UpdateItemStatusByID(ctx, it.ID, "waiting_payment"); err != nil {
			return apierror.ErrInternal
		}
		if it.FulfillmentStep < preOrderStepSecondPayment {
			if err := s.store.UpdateOrderItemStep(ctx, it.ID, preOrderStepSecondPayment); err != nil {
				return apierror.ErrInternal
			}
		}
	}

	return nil
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
