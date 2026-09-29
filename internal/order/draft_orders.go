package order

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

func (s *service) ListWebsiteDraftOrders(ctx context.Context) ([]WebsiteDraftOrderDTO, error) {
	drafts, err := s.shopClient.ListOpenDraftOrders(ctx)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	out := make([]WebsiteDraftOrderDTO, 0)
	for _, draft := range drafts {
		if !isWebsiteDraft(draft.CustomAttributes) {
			continue
		}
		currency := draft.CurrencyCode
		if currency == "" {
			currency = "USD"
		}
		out = append(out, WebsiteDraftOrderDTO{
			ID:         draft.ID,
			Name:       draft.Name,
			Status:     draft.Status,
			Email:      draft.Email,
			InvoiceURL: draft.InvoiceURL,
			CreatedAt:  draft.CreatedAt,
			Total:      draft.TotalAmount,
			Currency:   currency,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

func (s *service) GetWebsiteDraftOrder(ctx context.Context, id string) (*WebsiteDraftOrderDetailDTO, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, apierror.New(400, "bad_request", "draft id is required")
	}

	draft, err := s.shopClient.GetDraftOrder(ctx, id)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load shopify draft order",
			slog.String("draft_order_id", id),
			slog.Any("error", err))
		return nil, apierror.ErrInternal
	}
	return websiteDraftDetail(draft)
}

func websiteDraftDetail(draft *shopify.DraftOrderDetail) (*WebsiteDraftOrderDetailDTO, error) {
	if draft == nil || !isWebsiteDraft(draft.CustomAttributes) {
		return nil, apierror.ErrNotFound
	}

	currency := draft.CurrencyCode
	if currency == "" {
		currency = "USD"
	}
	items := make([]WebsiteDraftLineItemDTO, 0, len(draft.LineItems))
	for _, line := range draft.LineItems {
		items = append(items, WebsiteDraftLineItemDTO{
			Title:     line.Title,
			SKU:       line.SKU,
			Quantity:  line.Quantity,
			UnitPrice: line.UnitPrice,
			LineTotal: line.LineTotal,
			VariantID: draftLineVariantID(line),
			Kind:      draftLineKind(line),
		})
	}

	return &WebsiteDraftOrderDetailDTO{
		ID:              draft.ID,
		Name:            draft.Name,
		Status:          draft.Status,
		Email:           draft.Email,
		InvoiceURL:      draft.InvoiceURL,
		CreatedAt:       draft.CreatedAt,
		Note:            draft.Note,
		Currency:        currency,
		Subtotal:        draft.Subtotal,
		TotalTax:        draft.TotalTax,
		Total:           draft.Total,
		ShippingTitle:   draft.ShippingTitle,
		ShippingAmount:  draft.ShippingAmount,
		ShippingMethod:  draftAttrValue(draft.CustomAttributes, "preorder_shipping_method"),
		Origin:          draftAttrValue(draft.CustomAttributes, "preorder_warehouse_origin"),
		ShipTogether:    draftAttrValue(draft.CustomAttributes, "ship_together") == "true",
		OrderName:       draft.OrderName,
		ShippingAddress: draftAddressDTO(draft.ShippingAddress),
		BillingAddress:  draftAddressDTO(draft.BillingAddress),
		LineItems:       items,
	}, nil
}

func draftAddressDTO(addr *shopify.DraftOrderAddress) *AddressDTO {
	if addr == nil {
		return nil
	}
	return &AddressDTO{
		FirstName: addr.FirstName,
		LastName:  addr.LastName,
		Company:   addr.Company,
		Address1:  addr.Address1,
		Address2:  addr.Address2,
		City:      addr.City,
		Province:  addr.Province,
		Country:   addr.Country,
		Zip:       addr.Zip,
		Phone:     addr.Phone,
	}
}

func draftLineKind(line shopify.DraftOrderLine) string {
	for _, attr := range line.CustomAttributes {
		if attr.Key == "charge_type" && attr.Value == "pre_order_shipping_deposit" {
			return "shipping_deposit"
		}
		if attr.Key == "type" && attr.Value == "preorder_dp" {
			return "pre_order"
		}
	}
	if line.VariantID == "" {
		return "shipping_deposit"
	}
	return "ship_ready"
}

func draftLineVariantID(line shopify.DraftOrderLine) string {
	if line.VariantID != "" {
		return line.VariantID
	}
	return draftAttrValue(line.CustomAttributes, "variant_ref")
}

func draftAttrValue(attrs []shopify.AttributeInput, key string) string {
	for _, attr := range attrs {
		if attr.Key == key {
			return attr.Value
		}
	}
	return ""
}

func isWebsiteDraft(attrs []shopify.AttributeInput) bool {
	for _, attr := range attrs {
		if attr.Key == shopify.WholesaleSourceAttribute.Key && attr.Value == shopify.WholesaleSourceAttribute.Value {
			return true
		}
	}
	return false
}
