package order

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/email"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
)

func normalizeLineItemTitle(title string) string {
	return strings.TrimSpace(strings.ToLower(title))
}

// SyncFulfillmentOrdersFromShopify fetches FO/FOLI from Shopify and persists them locally.
// Maps pre_order line items by variant GID, or by title when Shopify returns variant null (common for deposit/preorder lines).
func (s *service) SyncFulfillmentOrdersFromShopify(ctx context.Context, orderID, shopifyOrderID string) error {
	if shopifyOrderID == "" {
		return nil
	}

	o, err := s.store.GetOrder(ctx, orderID, "")
	if err != nil || o == nil {
		return err
	}

	preOrderVariantToItem := make(map[string]string)
	preOrderTitleToItem := make(map[string]string)
	for _, it := range o.Items {
		if it.Type != "pre_order" {
			continue
		}
		preOrderVariantToItem[it.ShopifyVariantID] = it.ID
		if it.Title != nil && *it.Title != "" {
			preOrderTitleToItem[normalizeLineItemTitle(*it.Title)] = it.ID
		}
	}
	if len(preOrderVariantToItem) == 0 {
		return nil
	}

	fos, err := s.shopClient.FetchFulfillmentOrders(ctx, shopifyOrderID)
	if err != nil {
		slog.WarnContext(ctx, "failed to fetch fulfillment orders from Shopify", slog.String("order_id", orderID), slog.Any("error", err))
		return err
	}

	resolveItemID := func(li shopify.FulfillmentOrderLineItemData) (string, bool) {
		if li.VariantGID != "" {
			if itemID, ok := preOrderVariantToItem[li.VariantGID]; ok {
				return itemID, true
			}
		}
		if li.LineItemTitle != "" {
			if itemID, ok := preOrderTitleToItem[normalizeLineItemTitle(li.LineItemTitle)]; ok {
				return itemID, true
			}
		}
		return "", false
	}

	for _, fo := range fos {
		var synced []SyncedFulfillmentOrderLineItem
		for _, li := range fo.LineItems {
			itemID, ok := resolveItemID(li)
			if !ok {
				continue
			}
			synced = append(synced, SyncedFulfillmentOrderLineItem{
				ShopifyFulfillmentOrderLineItemID: li.ID,
				OrderLineItemID:                   itemID,
				TotalQuantity:                     li.TotalQuantity,
				RemainingQuantity:                 li.RemainingQuantity,
			})
		}
		if len(synced) == 0 {
			continue
		}
		var locName *string
		if fo.AssignedLocationName != "" {
			locName = &fo.AssignedLocationName
		}
		if err := s.store.UpsertFulfillmentOrder(ctx, orderID, fo.ID, fo.Status, locName, synced); err != nil {
			slog.WarnContext(ctx, "failed to upsert fulfillment order", slog.String("fo_id", fo.ID), slog.Any("error", err))
		}
	}
	return nil
}

// paidShipmentQtyByLine returns how many units of each line item are covered by
// paid pre-order group invoices. Multi-batch orders may have some groups paid
// while others are still awaiting shipping calculation or second payment.
func (s *service) paidShipmentQtyByLine(ctx context.Context, orderID string) (map[string]int, error) {
	shipments, err := s.store.GetPreorderShipments(ctx, orderID)
	if err != nil {
		return nil, err
	}
	return paidQtyByLineFromShipments(shipments), nil
}

func paidQtyByLineFromShipments(shipments []PreorderShipment) map[string]int {
	paidQtyByLine := make(map[string]int)
	for _, sh := range shipments {
		if sh.InvoicePaidAt == nil {
			continue
		}
		for _, p := range sh.PackingItems {
			qty := p.Quantity
			if qty < 1 {
				qty = 1
			}
			paidQtyByLine[p.OrderLineItemID] += qty
		}
	}
	return paidQtyByLine
}

func isPreorderLineReadyForFulfillment(step, paidQty, requestQty int) bool {
	return step >= preOrderStepShipped || paidQty >= requestQty
}

func (s *service) CreatePreorderFulfillment(ctx context.Context, userID, orderID string, req CreateFulfillmentRequest) (*FulfillmentDTO, error) {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}
	if o.ShopifyOrderID == nil || *o.ShopifyOrderID == "" {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Order has no Shopify order ID")
	}

	itemMap := make(map[string]OrderItem)
	for _, it := range o.Items {
		if it.Type == "pre_order" {
			itemMap[it.ID] = it
		}
	}
	if len(itemMap) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Order has no pre-order items")
	}

	paidQtyByLine, err := s.paidShipmentQtyByLine(ctx, orderID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	lineItemIDs := make([]string, 0, len(req.Items))
	for _, ri := range req.Items {
		it, ok := itemMap[ri.LineItemID]
		if !ok {
			return nil, apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Invalid line item: %s", ri.LineItemID))
		}
		// Only the items being fulfilled must be ready — other batches in the
		// same order may still be on calculate-shipping / second-payment.
		if !isPreorderLineReadyForFulfillment(it.FulfillmentStep, paidQtyByLine[ri.LineItemID], ri.Quantity) {
			return nil, apierror.New(http.StatusBadRequest, "invalid_transition", "Pre-order items must reach step 4 before fulfillment")
		}
		lineItemIDs = append(lineItemIDs, ri.LineItemID)
	}

	if err := s.SyncFulfillmentOrdersFromShopify(ctx, orderID, *o.ShopifyOrderID); err != nil {
		slog.WarnContext(ctx, "FO sync failed before fulfillment, continuing with cached data", slog.Any("error", err))
	}

	folis, err := s.store.GetFOLIByOrderLineItemIDs(ctx, orderID, lineItemIDs)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	foliByLineItem := pickBestFOLIByLineItem(folis)

	trackingURL := req.TrackingURL
	if trackingURL == "" {
		trackingURL = shipping.BuildTrackingURL(req.TrackingCompany, req.TrackingNumber)
	}
	carrier := shipping.NormalizeCarrier(req.TrackingCompany)

	localOnly := false
	foGroups := make(map[string][]struct {
		foliID string
		qty    int
	})

	for _, ri := range req.Items {
		foli, ok := foliByLineItem[ri.LineItemID]
		if !ok {
			// No Shopify FO line after sync — still allow admin to record tracking locally.
			slog.WarnContext(ctx, "no FOLI for pre-order item; using local fulfillment",
				slog.String("order_id", orderID),
				slog.String("line_item_id", ri.LineItemID),
			)
			localOnly = true
			continue
		}
		if ri.Quantity <= foli.RemainingQuantity {
			foGroups[foli.FulfillmentOrderID] = append(foGroups[foli.FulfillmentOrderID], struct {
				foliID string
				qty    int
			}{foliID: foli.ShopifyFulfillmentOrderLineItemID, qty: ri.Quantity})
			continue
		}
		if foli.RemainingQuantity > 0 {
			return nil, apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Quantity %d exceeds remaining %d for item %s", ri.Quantity, foli.RemainingQuantity, ri.LineItemID))
		}

		// Remaining is 0 on Shopify (FO closed/already fulfilled there) but admin still
		// needs to record tracking when we have no local fulfillment for this qty.
		localFulfilled := s.localFulfilledQuantity(ctx, orderID, ri.LineItemID)
		it := itemMap[ri.LineItemID]
		if localFulfilled >= it.Quantity {
			return nil, apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Item %s is already fully fulfilled", ri.LineItemID))
		}
		if localFulfilled+ri.Quantity > it.Quantity {
			return nil, apierror.New(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Quantity %d exceeds unfulfilled %d for item %s", ri.Quantity, it.Quantity-localFulfilled, ri.LineItemID))
		}
		slog.WarnContext(ctx, "Shopify FOLI remaining is 0; recording pre-order fulfillment locally",
			slog.String("order_id", orderID),
			slog.String("line_item_id", ri.LineItemID),
			slog.Int("requested_qty", ri.Quantity),
		)
		localOnly = true
	}

	var shopifyFID *string
	if !localOnly {
		fos, err := s.store.GetFulfillmentOrdersByOrderID(ctx, orderID)
		if err != nil {
			return nil, apierror.ErrInternal
		}
		foShopifyID := make(map[string]string)
		for _, fo := range fos {
			foShopifyID[fo.ID] = fo.ShopifyFulfillmentOrderID
		}

		var shopifyInput shopify.CreateFulfillmentV2Input
		shopifyInput.NotifyCustomer = req.NotifyCustomer
		shopifyInput.TrackingNumber = req.TrackingNumber
		shopifyInput.TrackingCompany = carrier
		shopifyInput.TrackingURL = trackingURL

		for foLocalID, items := range foGroups {
			shopifyFOID, ok := foShopifyID[foLocalID]
			if !ok {
				return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Fulfillment order not found")
			}
			var foliInput shopify.FulfillmentV2LineItemInput
			foliInput.FulfillmentOrderID = shopifyFOID
			for _, it := range items {
				foliInput.FulfillmentOrderLineItems = append(foliInput.FulfillmentOrderLineItems, struct {
					ID       string
					Quantity int
				}{ID: it.foliID, Quantity: it.qty})
			}
			shopifyInput.LineItemsByFulfillmentOrder = append(shopifyInput.LineItemsByFulfillmentOrder, foliInput)
		}

		result, err := s.shopClient.CreateFulfillmentV2(ctx, shopifyInput)
		if err != nil {
			return nil, apierror.New(http.StatusBadGateway, "shopify_error", err.Error())
		}
		shopifyFID = &result.FulfillmentID
	}

	seq, err := s.store.GetNextFulfillmentSequence(ctx, orderID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	now := time.Now()
	trackingNum := req.TrackingNumber
	trackingURLPtr := trackingURL
	carrierPtr := carrier
	status := "fulfilled"
	f := &Fulfillment{
		OrderID:              orderID,
		BatchID:              normalizeBatchIDPtr(req.BatchID),
		ShopifyFulfillmentID: shopifyFID,
		SequenceNumber:       seq,
		TrackingNumber:       &trackingNum,
		TrackingURL:          &trackingURLPtr,
		TrackingCompany:      &carrierPtr,
		Status:               status,
		NotifyCustomer:       req.NotifyCustomer,
		FulfilledAt:          &now,
	}

	var flis []FulfillmentLineItem
	for _, ri := range req.Items {
		flis = append(flis, FulfillmentLineItem{
			OrderLineItemID: ri.LineItemID,
			Quantity:        ri.Quantity,
		})
	}

	if err := s.store.UpsertFulfillmentByShopifyID(ctx, f, flis); err != nil {
		return nil, apierror.ErrInternal
	}

	for _, ri := range req.Items {
		if !localOnly {
			if foli, ok := foliByLineItem[ri.LineItemID]; ok {
				_ = s.store.DecrementFOLIRemaining(ctx, foli.ID, ri.Quantity)
			}
		}
		fulfillmentStep := preOrderStepShipped
		_ = s.store.UpdateOrderItemTracking(ctx, ri.LineItemID, trackingNum, trackingURLPtr, carrierPtr, "Package shipped", "shipped", fulfillmentStep, &now)
	}

	if req.NotifyCustomer {
		go func() {
			bgCtx := context.Background()
			user, _ := s.authStore.GetUserByID(bgCtx, o.CustomerID)
			if user != nil {
				_ = s.emailService.SendShipmentDispatched(bgCtx, user.Email, email.ShipmentEmailData{
					CustomerName:   "Customer",
					OrderNumber:    o.OrderNumber,
					Carrier:        carrier,
					TrackingNumber: trackingNum,
					TrackingURL:    trackingURL,
				})
			}
		}()
	}

	return s.fulfillmentToDTO(ctx, o, f, flis, itemMap)
}

// pickBestFOLIByLineItem keeps the FOLI with the highest remaining quantity per line.
func pickBestFOLIByLineItem(folis []FulfillmentOrderLineItem) map[string]FulfillmentOrderLineItem {
	out := make(map[string]FulfillmentOrderLineItem, len(folis))
	for _, foli := range folis {
		existing, ok := out[foli.OrderLineItemID]
		if !ok || foli.RemainingQuantity > existing.RemainingQuantity {
			out[foli.OrderLineItemID] = foli
		}
	}
	return out
}

func (s *service) localFulfilledQuantity(ctx context.Context, orderID, lineItemID string) int {
	fulfillments, err := s.store.GetFulfillmentsByOrderID(ctx, orderID)
	if err != nil {
		return 0
	}
	fulfilled := 0
	for _, f := range fulfillments {
		for _, fli := range f.LineItems {
			if fli.OrderLineItemID == lineItemID {
				fulfilled += fli.Quantity
			}
		}
	}
	return fulfilled
}

func (s *service) MarkFulfillmentDelivered(ctx context.Context, userID, orderID, fulfillmentID string) error {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return apierror.ErrInternal
	}
	if o == nil {
		return apierror.ErrNotFound
	}

	f, err := s.store.GetFulfillmentByID(ctx, fulfillmentID)
	if err != nil || f == nil || f.OrderID != orderID {
		return apierror.ErrNotFound
	}
	if f.Status == "delivered" {
		return nil
	}

	if f.ShopifyFulfillmentID != nil && *f.ShopifyFulfillmentID != "" {
		if err := s.shopClient.CreateFulfillmentEvent(ctx, *f.ShopifyFulfillmentID, "DELIVERED"); err != nil {
			return apierror.New(http.StatusBadGateway, "shopify_error", err.Error())
		}
	}

	now := time.Now()
	if err := s.store.MarkFulfillmentDelivered(ctx, fulfillmentID, now); err != nil {
		return apierror.ErrInternal
	}

	for _, fli := range f.LineItems {
		_ = s.store.UpdateOrderItemTracking(ctx, fli.OrderLineItemID, derefStr(f.TrackingNumber), derefStr(f.TrackingURL), derefStr(f.TrackingCompany), "Delivered", "delivered", preOrderStepDelivered, &now)

		var item OrderItem
		for _, it := range o.Items {
			if it.ID == fli.OrderLineItemID {
				item = it
				break
			}
		}
		received := item.ItemsReceived + fli.Quantity
		if item.Quantity > 0 && received > item.Quantity {
			received = item.Quantity
		}
		if received < fli.Quantity {
			received = fli.Quantity
		}
		if err := s.store.UpdateOrderItemReceived(ctx, fli.OrderLineItemID, received, preOrderStepDelivered); err != nil {
			slog.WarnContext(ctx, "failed to update items_received on deliver",
				slog.String("item_id", fli.OrderLineItemID),
				slog.Any("error", err),
			)
		}
		for i := range o.Items {
			if o.Items[i].ID == fli.OrderLineItemID {
				o.Items[i].ItemStatus = "delivered"
				o.Items[i].ItemsReceived = received
				o.Items[i].FulfillmentStep = preOrderStepDelivered
				break
			}
		}
	}

	allDelivered := true
	for _, it := range o.Items {
		if it.Type != "pre_order" {
			continue
		}
		if it.ItemStatus == "delivered" && it.ItemsReceived >= it.Quantity {
			continue
		}
		remaining := s.computeRemainingQuantity(ctx, orderID, it)
		if remaining > 0 || it.ItemsReceived < it.Quantity {
			allDelivered = false
			break
		}
	}
	if allDelivered {
		_ = s.store.UpdateOrderStatus(ctx, orderID, "completed", o.FinancialStatus, "fulfilled")
	}

	return nil
}

func (s *service) computeRemainingQuantity(ctx context.Context, orderID string, item OrderItem) int {
	localFulfilled := s.localFulfilledQuantity(ctx, orderID, item.ID)
	localLeft := item.Quantity - localFulfilled
	if localLeft < 0 {
		localLeft = 0
	}

	folis, err := s.store.GetFOLIByOrderLineItemIDs(ctx, orderID, []string{item.ID})
	if err == nil && len(folis) > 0 {
		best := pickBestFOLIByLineItem(folis)[item.ID]
		// Shopify FO may already be closed (remaining 0) while we still need to
		// record tracking locally — don't hide fulfillable qty from admin UI.
		if best.RemainingQuantity <= 0 && localFulfilled == 0 {
			return localLeft
		}
		if best.RemainingQuantity < localLeft {
			return best.RemainingQuantity
		}
		return localLeft
	}
	return localLeft
}

func (s *service) enrichRemainingQuantities(ctx context.Context, orderID string, items []OrderItemDetail) {
	for i := range items {
		if items[i].Type != "pre_order" {
			continue
		}
		oItem := OrderItem{ID: items[i].ID, Quantity: items[i].Quantity}
		items[i].RemainingQuantity = s.computeRemainingQuantity(ctx, orderID, oItem)
	}
}

func (s *service) loadFulfillmentDTOs(ctx context.Context, o *Order) []FulfillmentDTO {
	fulfillments, err := s.store.GetFulfillmentsByOrderID(ctx, o.ID)
	if err != nil || len(fulfillments) == 0 {
		return []FulfillmentDTO{}
	}

	itemMap := make(map[string]OrderItem)
	for _, it := range o.Items {
		itemMap[it.ID] = it
	}

	var dtos []FulfillmentDTO
	for _, f := range fulfillments {
		dto, err := s.fulfillmentToDTO(ctx, o, &f, f.LineItems, itemMap)
		if err == nil && dto != nil {
			dtos = append(dtos, *dto)
		}
	}
	return dtos
}

func (s *service) fulfillmentToDTO(ctx context.Context, o *Order, f *Fulfillment, flis []FulfillmentLineItem, itemMap map[string]OrderItem) (*FulfillmentDTO, error) {
	dto := &FulfillmentDTO{
		ID:             f.ID,
		DisplayID:      fmt.Sprintf("#%s-F%d", o.OrderNumber, f.SequenceNumber),
		SequenceNumber: f.SequenceNumber,
		BatchID:        f.BatchID,
		Status:         f.Status,
	}
	if f.TrackingNumber != nil {
		dto.TrackingNumber = *f.TrackingNumber
	}
	if f.TrackingURL != nil {
		dto.TrackingURL = *f.TrackingURL
	}
	if f.TrackingCompany != nil {
		dto.TrackingCompany = *f.TrackingCompany
	}
	if f.ShipmentStatus != nil {
		dto.ShipmentStatus = *f.ShipmentStatus
	}
	if f.FulfilledAt != nil {
		dto.FulfilledAt = f.FulfilledAt.Format(time.RFC3339)
	}
	if f.DeliveredAt != nil {
		dto.DeliveredAt = f.DeliveredAt.Format(time.RFC3339)
	}

	for _, fli := range flis {
		item := itemMap[fli.OrderLineItemID]
		title := ""
		if item.Title != nil {
			title = *item.Title
		}
		var unitPrice string
		if item.UnitPrice != nil {
			unitPrice = fmt.Sprintf("%.2f", *item.UnitPrice)
		}
		dto.LineItems = append(dto.LineItems, FulfillmentLineItemDTO{
			LineItemID: fli.OrderLineItemID,
			Title:      title,
			Quantity:   fli.Quantity,
			ImageSrc:   item.ImageSrc,
			UnitPrice:  unitPrice,
		})
	}
	return dto, nil
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
