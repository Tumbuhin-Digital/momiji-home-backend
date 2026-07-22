package order

import (
	"context"
	"fmt"
	"sort"
)

// buildFulfillmentGroups splits order items into Ship Ready, Pre-Order (unbatched),
// and one card per allocated preorder batch.
func (s *service) buildFulfillmentGroups(
	ctx context.Context,
	o *Order,
	detailsByID map[string]OrderItemDetail,
	allFulfillments []FulfillmentDTO,
) ([]FulfillmentGroupDTO, *SecondPaymentDTO, error) {
	var shipReadySlices []OrderLineSliceDTO
	preItems := make([]OrderItem, 0)
	preLineIDs := make([]string, 0)

	for _, it := range o.Items {
		detail, ok := detailsByID[it.ID]
		if !ok {
			continue
		}
		if it.Type == "ship_ready" {
			shipReadySlices = append(shipReadySlices, detailToSlice(detail, it.Quantity))
			continue
		}
		if it.Type == "pre_order" {
			preItems = append(preItems, it)
			preLineIDs = append(preLineIDs, it.ID)
		}
	}

	groups := make([]FulfillmentGroupDTO, 0)
	if len(shipReadySlices) > 0 {
		groups = append(groups, FulfillmentGroupDTO{
			Key:          "ship_ready",
			Kind:         FulfillmentGroupShipReady,
			Title:        "Ship Ready",
			LineSlices:   shipReadySlices,
			Fulfillments: filterFulfillmentsForSlices(allFulfillments, shipReadySlices),
		})
	}

	if len(preItems) == 0 {
		return groups, nil, nil
	}

	allocatedQty := make(map[string]int) // lineItemID -> sum allocated
	batchSlices := make(map[string][]OrderLineSliceDTO)
	batchNames := make(map[string]string)

	if s.batchService != nil {
		allocs, err := s.batchService.GetCommittedAllocationsByOrderLineItemIDs(ctx, preLineIDs)
		if err != nil {
			return nil, nil, err
		}
		batchIDSet := make(map[string]struct{})
		for _, a := range allocs {
			if a.OrderLineItemID == nil {
				continue
			}
			lineID := *a.OrderLineItemID
			allocatedQty[lineID] += a.Quantity
			batchIDSet[a.BatchID] = struct{}{}
			detail := detailsByID[lineID]
			batchSlices[a.BatchID] = append(batchSlices[a.BatchID], detailToSlice(detail, a.Quantity))
		}

		batchIDs := make([]string, 0, len(batchIDSet))
		for id := range batchIDSet {
			batchIDs = append(batchIDs, id)
		}
		batches, err := s.batchService.GetBatchesByIDs(ctx, batchIDs)
		if err != nil {
			return nil, nil, err
		}
		for _, b := range batches {
			batchNames[b.ID] = b.Name
		}
	}

	var unbatchedSlices []OrderLineSliceDTO
	for _, it := range preItems {
		remaining := it.Quantity - allocatedQty[it.ID]
		if remaining <= 0 {
			continue
		}
		unbatchedSlices = append(unbatchedSlices, detailToSlice(detailsByID[it.ID], remaining))
	}

	shipments, err := s.store.GetPreorderShipments(ctx, o.ID)
	if err != nil {
		return nil, nil, err
	}
	shipmentByBatch := make(map[string]*PreorderShipment) // "" = unbatched
	for i := range shipments {
		sh := &shipments[i]
		key := ""
		if sh.BatchID != nil {
			key = *sh.BatchID
		}
		shipmentByBatch[key] = sh
	}

	// Stable batch card order by batch name then id.
	batchKeys := make([]string, 0, len(batchSlices))
	for id := range batchSlices {
		batchKeys = append(batchKeys, id)
	}
	sort.Slice(batchKeys, func(i, j int) bool {
		ni, nj := batchNames[batchKeys[i]], batchNames[batchKeys[j]]
		if ni == nj {
			return batchKeys[i] < batchKeys[j]
		}
		return ni < nj
	})

	for _, batchID := range batchKeys {
		slices := batchSlices[batchID]
		name := batchNames[batchID]
		if name == "" {
			name = "Batch"
		}
		bid := batchID
		group := FulfillmentGroupDTO{
			Key:        "batch:" + batchID,
			Kind:       FulfillmentGroupPreorderBatch,
			Title:      "Pre-Order (Batch)",
			BatchID:    &bid,
			BatchName:  name,
			LineSlices: slices,
		}
		if sh := shipmentByBatch[batchID]; sh != nil {
			dto := s.toPreorderShipmentDTO(sh)
			group.Shipment = &dto
		}
		group.Fulfillments = filterFulfillmentsForSlices(allFulfillments, slices)
		enrichGroupSecondPayment(&group, o)
		groups = append(groups, group)
	}

	if len(unbatchedSlices) > 0 {
		group := FulfillmentGroupDTO{
			Key:          "preorder_unbatched",
			Kind:         FulfillmentGroupPreorderUnbatched,
			Title:        "Pre-Order",
			LineSlices:   unbatchedSlices,
			Fulfillments: filterFulfillmentsForSlices(allFulfillments, unbatchedSlices),
		}
		if sh := shipmentByBatch[""]; sh != nil {
			dto := s.toPreorderShipmentDTO(sh)
			group.Shipment = &dto
		}
		enrichGroupSecondPayment(&group, o)
		groups = append(groups, group)
	}

	second := s.buildSecondPaymentSummary(o, groups)
	return groups, second, nil
}

func enrichGroupSecondPayment(group *FulfillmentGroupDTO, o *Order) {
	if group == nil {
		return
	}
	fullQtyByLine := make(map[string]int)
	for _, it := range o.Items {
		if it.Type == "pre_order" {
			fullQtyByLine[it.ID] = it.Quantity
		}
	}

	balance := 0.0
	for _, sl := range group.LineSlices {
		if sl.BalanceDue == nil || sl.Quantity < 1 {
			continue
		}
		var lineBal float64
		fmt.Sscanf(*sl.BalanceDue, "%f", &lineBal)
		fullQty := fullQtyByLine[sl.LineItemID]
		if fullQty < 1 {
			fullQty = sl.Quantity
		}
		balance += lineBal * float64(sl.Quantity) / float64(fullQty)
	}
	balance = float64(int(balance*100+0.5)) / 100
	balStr := fmt.Sprintf("%.2f", balance)
	group.GroupBalanceDue = &balStr

	status := "pending"
	canRequest := false
	if group.Shipment != nil {
		if group.Shipment.FinalShippingPrice != nil {
			group.GroupShipping = group.Shipment.FinalShippingPrice
			status = "ready"
			canRequest = group.Shipment.InvoiceSentAt == nil
		}
		if group.Shipment.InvoicePaidAt != nil {
			status = "paid"
			canRequest = false
		} else if group.Shipment.InvoiceSentAt != nil {
			status = "invoiced"
			canRequest = false
		}
	}
	group.SecondPaymentStatus = status
	group.CanRequestSecondPayment = canRequest
}

func detailToSlice(detail OrderItemDetail, qty int) OrderLineSliceDTO {
	remaining := detail.RemainingQuantity
	if remaining > qty {
		remaining = qty
	}
	return OrderLineSliceDTO{
		LineItemID:        detail.ID,
		VariantID:         detail.VariantID,
		Type:              detail.Type,
		Quantity:          qty,
		RemainingQuantity: remaining,
		ItemStatus:        detail.ItemStatus,
		FulfillmentStep:   detail.FulfillmentStep,
		Title:             detail.Title,
		UnitPrice:         detail.UnitPrice,
		AmountCharged:     detail.AmountCharged,
		BalanceDue:        detail.BalanceDue,
		DpAmount:          detail.DpAmount,
		FinalAmount:       detail.FinalAmount,
		ImageSrc:          detail.ImageSrc,
		SKU:               detail.SKU,
		WeightKg:          detail.WeightKg,
		WidthCm:           detail.WidthCm,
		HeightCm:          detail.HeightCm,
		DepthCm:           detail.DepthCm,
		TrackingNumber:    detail.TrackingNumber,
		TrackingURL:       detail.TrackingURL,
		TrackingCompany:   detail.TrackingCompany,
		TrackingLastEvent: detail.TrackingLastEvent,
	}
}

func filterFulfillmentsForSlices(all []FulfillmentDTO, slices []OrderLineSliceDTO) []FulfillmentDTO {
	if len(all) == 0 || len(slices) == 0 {
		return []FulfillmentDTO{}
	}
	wanted := make(map[string]int, len(slices))
	for _, sl := range slices {
		wanted[sl.LineItemID] += sl.Quantity
	}
	out := make([]FulfillmentDTO, 0)
	for _, f := range all {
		overlap := false
		for _, li := range f.LineItems {
			if wanted[li.LineItemID] > 0 {
				overlap = true
				break
			}
		}
		if overlap {
			out = append(out, f)
		}
	}
	return out
}

func (s *service) buildSecondPaymentSummary(o *Order, groups []FulfillmentGroupDTO) *SecondPaymentDTO {
	preGroups := make([]FulfillmentGroupDTO, 0)
	for _, g := range groups {
		if g.Kind == FulfillmentGroupPreorderBatch || g.Kind == FulfillmentGroupPreorderUnbatched {
			preGroups = append(preGroups, g)
		}
	}
	if len(preGroups) == 0 {
		return nil
	}

	configured := 0
	shippingTotal := 0.0
	anyInvoiced := false
	anyPaid := false
	anyReady := false
	for _, g := range preGroups {
		if g.Shipment != nil && g.Shipment.FinalShippingPrice != nil {
			configured++
			var price float64
			fmt.Sscanf(*g.Shipment.FinalShippingPrice, "%f", &price)
			shippingTotal += price
		}
		switch g.SecondPaymentStatus {
		case "paid":
			anyPaid = true
			anyInvoiced = true
		case "invoiced":
			anyInvoiced = true
		case "ready":
			anyReady = true
		}
	}

	status := "pending"
	if anyPaid && configured == len(preGroups) {
		allPaid := true
		for _, g := range preGroups {
			if g.SecondPaymentStatus != "paid" {
				allPaid = false
				break
			}
		}
		if allPaid {
			status = "paid"
		} else {
			status = "invoiced"
		}
	} else if anyInvoiced {
		status = "invoiced"
	} else if anyReady || configured > 0 {
		status = "ready"
	}

	// Order-level can_request is true if ANY group can request (informational; UI uses per-group).
	canRequest := false
	for _, g := range preGroups {
		if g.CanRequestSecondPayment {
			canRequest = true
			break
		}
	}

	return &SecondPaymentDTO{
		TotalBalanceDue:  fmt.Sprintf("%.2f", o.TotalBalanceDue),
		ShippingTotal:    fmt.Sprintf("%.2f", shippingTotal),
		CanRequest:       canRequest,
		Status:           status,
		ConfiguredGroups: configured,
		TotalGroups:      len(preGroups),
	}
}

// resolveGroupSlices returns the order items (with Quantity set to slice qty) for a batch scope.
func (s *service) resolveGroupSlices(ctx context.Context, o *Order, batchID *string) ([]OrderItem, error) {
	preItems := s.getPreOrderItems(o)
	if len(preItems) == 0 {
		return nil, nil
	}

	lineIDs := make([]string, 0, len(preItems))
	itemByID := make(map[string]OrderItem, len(preItems))
	for _, it := range preItems {
		lineIDs = append(lineIDs, it.ID)
		itemByID[it.ID] = it
	}

	allocatedQty := make(map[string]int)
	var allocs []struct {
		BatchID  string
		LineID   string
		Quantity int
	}

	if s.batchService != nil {
		raw, err := s.batchService.GetCommittedAllocationsByOrderLineItemIDs(ctx, lineIDs)
		if err != nil {
			return nil, err
		}
		for _, a := range raw {
			if a.OrderLineItemID == nil {
				continue
			}
			lineID := *a.OrderLineItemID
			allocatedQty[lineID] += a.Quantity
			allocs = append(allocs, struct {
				BatchID  string
				LineID   string
				Quantity int
			}{BatchID: a.BatchID, LineID: lineID, Quantity: a.Quantity})
		}
	}

	wantBatch := ""
	if batchID != nil {
		wantBatch = *batchID
	}

	out := make([]OrderItem, 0)
	if wantBatch == "" {
		for _, it := range preItems {
			remaining := it.Quantity - allocatedQty[it.ID]
			if remaining <= 0 {
				continue
			}
			clone := it
			clone.Quantity = remaining
			out = append(out, clone)
		}
		return out, nil
	}

	for _, a := range allocs {
		if a.BatchID != wantBatch {
			continue
		}
		it, ok := itemByID[a.LineID]
		if !ok {
			continue
		}
		clone := it
		clone.Quantity = a.Quantity
		out = append(out, clone)
	}
	return out, nil
}

func normalizeBatchIDPtr(batchID *string) *string {
	if batchID == nil {
		return nil
	}
	if *batchID == "" {
		return nil
	}
	return batchID
}
