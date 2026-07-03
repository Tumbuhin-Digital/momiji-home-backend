package order

import (
	"context"
	"fmt"
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

func (s *service) CalculatePreorderShipping(ctx context.Context, userID, orderID string, req CalculatePreorderShippingRequest) (*CalculatePreorderShippingResponse, error) {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if o == nil {
		return nil, apierror.ErrNotFound
	}

	preItems := s.getPreOrderItems(o)
	if len(preItems) == 0 {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Order has no pre-order items")
	}

	if err := ValidatePacking(req.Packing, preItems); err != nil {
		return nil, err
	}

	itemMap := make(map[string]OrderItem)
	var variantIDs []string
	for _, it := range preItems {
		itemMap[it.ID] = it
		variantIDs = append(variantIDs, it.ShopifyVariantID)
	}

	dims, err := s.store.GetVariantDimensions(ctx, variantIDs)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	units := BuildPackableUnits(req.Packing, itemMap, dims)
	packages := shipping.BuildPackages(units)
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
	province := addr.Province

	origin, err := s.warehouseResolver.GetOrigin(ctx, warehouse.CodeEast)
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
			State:    province,
			Zip:      addr.Zip,
			Country:  addr.Country,
		},
		packages,
		s.zipLookup,
	)
	if err != nil {
		return nil, apierror.New(http.StatusInternalServerError, "shipping_rate_error", "Failed to fetch shipping rates from carriers")
	}

	totalBoxes, totalWeight := PackingTotals(units)
	liveEstimate := amount
	dbPacking := PackingToDBItems(req.Packing)

	existing, err := s.store.GetPreorderShipment(ctx, orderID)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	shipment := &PreorderShipment{
		OrderID:       orderID,
		TotalBoxes:    totalBoxes,
		TotalWeightLb: &totalWeight,
	}
	if existing != nil && existing.EstimatedShipping != nil {
		// Preserve checkout estimate; live rate is returned in the response only.
	} else {
		shipment.EstimatedShipping = &liveEstimate
	}

	if err := s.store.UpsertPreorderShipment(ctx, shipment, dbPacking); err != nil {
		return nil, apierror.ErrInternal
	}

	groundCode := s.shipstationCfg.GroundServiceCode
	if groundCode == "" {
		groundCode = "ups_ground"
	}

	return &CalculatePreorderShippingResponse{
		EstimatedShipping: fmt.Sprintf("%.2f", liveEstimate),
		TotalBoxes:        totalBoxes,
		TotalWeightLb:     fmt.Sprintf("%.2f", totalWeight),
		Packing:           req.Packing,
		ServiceCode:       groundCode,
		Currency:          currency,
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

	shipment, err := s.store.GetPreorderShipment(ctx, orderID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if shipment == nil || shipment.EstimatedShipping == nil {
		return nil, apierror.New(http.StatusBadRequest, "invalid_request", "Checkout shipping estimate required before setting final price")
	}

	if len(req.Packing) > 0 {
		preItems := s.getPreOrderItems(o)
		if err := ValidatePacking(req.Packing, preItems); err != nil {
			return nil, err
		}

		itemMap := make(map[string]OrderItem)
		var variantIDs []string
		for _, it := range preItems {
			itemMap[it.ID] = it
			variantIDs = append(variantIDs, it.ShopifyVariantID)
		}

		dims, dimErr := s.store.GetVariantDimensions(ctx, variantIDs)
		if dimErr != nil {
			return nil, apierror.ErrInternal
		}

		units := BuildPackableUnits(req.Packing, itemMap, dims)
		if len(shipping.BuildPackages(units)) == 0 {
			return nil, apierror.New(http.StatusBadRequest, "invalid_request", "No shippable boxes in packing plan")
		}

		totalBoxes, totalWeight := PackingTotals(units)
		packingShipment := &PreorderShipment{
			OrderID:           orderID,
			EstimatedShipping: shipment.EstimatedShipping,
			TotalBoxes:        totalBoxes,
			TotalWeightLb:     &totalWeight,
		}
		if err := s.store.UpsertPreorderShipment(ctx, packingShipment, PackingToDBItems(req.Packing)); err != nil {
			return nil, apierror.ErrInternal
		}
	}

	credit := math.Max(0, *shipment.EstimatedShipping-req.FinalShippingPrice)
	if err := s.store.UpdatePreorderShipping(ctx, orderID, req.FinalShippingPrice, req.ShippingNotes, credit); err != nil {
		return nil, apierror.ErrInternal
	}

	for _, it := range s.getPreOrderItems(o) {
		if it.FulfillmentStep < preOrderStepShippingConfigured {
			if err := s.store.UpdateItemStepByType(ctx, orderID, "pre_order", preOrderStepShippingConfigured); err != nil {
				return nil, apierror.ErrInternal
			}
			break
		}
	}

	updated, err := s.store.GetPreorderShipment(ctx, orderID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	dto := s.toPreorderShipmentDTO(updated)
	return &dto, nil
}

func (s *service) RequestSecondPayment(ctx context.Context, userID, orderID string) error {
	o, err := s.store.GetOrder(ctx, orderID, userID)
	if err != nil {
		return apierror.ErrInternal
	}
	if o == nil {
		return apierror.ErrNotFound
	}

	preItems := s.getPreOrderItems(o)
	if len(preItems) == 0 {
		return apierror.New(http.StatusBadRequest, "invalid_request", "Order has no pre-order items")
	}

	for _, it := range preItems {
		if it.FulfillmentStep != preOrderStepShippingConfigured {
			return apierror.New(http.StatusConflict, "invalid_transition", "Shipping must be configured before requesting second payment")
		}
	}

	shipment, err := s.store.GetPreorderShipment(ctx, orderID)
	if err != nil {
		return apierror.ErrInternal
	}
	if shipment == nil || shipment.FinalShippingPrice == nil {
		return apierror.New(http.StatusBadRequest, "invalid_request", "Final shipping price must be set before requesting payment")
	}

	var lineItemIDs []string
	for _, it := range preItems {
		lineItemIDs = append(lineItemIDs, it.ID)
	}

	if o.ShippingAddress == nil {
		return apierror.New(http.StatusBadRequest, "invalid_request", "Order has no shipping address")
	}

	shippingTitle := "UPS Ground"
	if o.ShippingMethod != nil && *o.ShippingMethod != "" {
		shippingTitle = *o.ShippingMethod
	}
	notes := ""
	if shipment.ShippingNotes != nil {
		notes = *shipment.ShippingNotes
	}

	opts := preorder.InvoiceOptions{
		ShippingTitle:   shippingTitle,
		ShippingPrice:   *shipment.FinalShippingPrice,
		ShippingNotes:   notes,
		ShippingAddress: orderShippingAddressToShopify(o.ShippingAddress),
	}

	if _, err := s.preorderService.InvoiceSettlementsWithShipping(ctx, lineItemIDs, opts); err != nil {
		return err
	}

	now := time.Now()
	if err := s.store.MarkPreorderInvoiceSent(ctx, orderID, now); err != nil {
		return apierror.ErrInternal
	}
	if err := s.store.UpdateOrderStatus(ctx, orderID, "on_progress", o.FinancialStatus, "waiting"); err != nil {
		return apierror.ErrInternal
	}
	if err := s.store.UpdateItemStatusByType(ctx, orderID, "pre_order", "waiting_payment"); err != nil {
		return apierror.ErrInternal
	}
	if err := s.store.UpdateItemStepByType(ctx, orderID, "pre_order", preOrderStepSecondPayment); err != nil {
		return apierror.ErrInternal
	}

	return nil
}

func orderShippingAddressToShopify(addr *customer.Address) *shopify.AddressInput {
	if addr == nil {
		return nil
	}

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

	return &shopify.AddressInput{
		FirstName: firstName,
		LastName:  lastName,
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
		TotalBoxes: shipment.TotalBoxes,
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
	if shipment.InvoiceSentAt != nil {
		v := shipment.InvoiceSentAt.Format(time.RFC3339)
		dto.InvoiceSentAt = &v
	}
	for _, p := range shipment.PackingItems {
		dto.Packing = append(dto.Packing, PackingItemDTO{
			LineItemID: p.OrderLineItemID,
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
