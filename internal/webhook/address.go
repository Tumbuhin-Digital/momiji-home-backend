package webhook

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/checkout"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
)

func resolveOrderShippingAddress(ctx context.Context, payload ShopifyOrderWebhook) (*ShopifyAddress, string) {
	if payload.ShippingAddress != nil && strings.TrimSpace(payload.ShippingAddress.Address1) != "" {
		return payload.ShippingAddress, "shopify_shipping"
	}

	for _, note := range payload.NoteAttributes {
		if note.Name != checkout.ShippingAddressNoteAttribute {
			continue
		}
		val, ok := note.Value.(string)
		if !ok || val == "" {
			continue
		}
		snap, err := checkout.ParseShippingAddressSnapshot(val)
		if err != nil {
			slog.WarnContext(ctx, "invalid checkout_shipping_address note attribute",
				slog.Any("error", err))
			continue
		}
		if !snap.IsValid() {
			continue
		}
		slog.InfoContext(ctx, "using checkout_shipping_address from note_attributes")
		return snapshotToShopifyAddress(snap), "checkout_note_attribute"
	}

	if payload.BillingAddress != nil && strings.TrimSpace(payload.BillingAddress.Address1) != "" {
		slog.InfoContext(ctx, "using billing_address as shipping fallback")
		return payload.BillingAddress, "shopify_billing"
	}

	return nil, ""
}

func snapshotToShopifyAddress(snap *checkout.ShippingAddressSnapshot) *ShopifyAddress {
	if snap == nil {
		return nil
	}
	return &ShopifyAddress{
		FirstName: snap.FirstName,
		LastName:  snap.LastName,
		Address1:  snap.Address1,
		Address2:  snap.Address2,
		City:      snap.City,
		Province:  snap.Province,
		Country:   snap.Country,
		Zip:       snap.Zip,
		Phone:     snap.Phone,
	}
}

func createCustomerShippingAddress(
	ctx context.Context,
	customerStore customer.CustomerStore,
	customerID string,
	src *ShopifyAddress,
) *string {
	if src == nil || strings.TrimSpace(src.Address1) == "" ||
		strings.TrimSpace(src.City) == "" || strings.TrimSpace(src.Zip) == "" {
		return nil
	}

	country := strings.TrimSpace(src.Country)
	if country == "" {
		country = "US"
	}
	province := strings.TrimSpace(src.Province)
	if province == "" {
		province = "N/A"
	}

	addrID := uuid.NewString()
	addr := &customer.Address{
		ID:         addrID,
		CustomerID: customerID,
		Address1:   strings.TrimSpace(src.Address1),
		City:       strings.TrimSpace(src.City),
		Province:   province,
		Country:    country,
		Zip:        strings.TrimSpace(src.Zip),
		IsDefault:  true,
	}
	if src.FirstName != "" {
		firstName := src.FirstName
		addr.FirstName = &firstName
	}
	if src.LastName != "" {
		lastName := src.LastName
		addr.LastName = &lastName
	}
	if src.Address2 != "" {
		address2 := src.Address2
		addr.Address2 = &address2
	}
	if src.Phone != "" {
		phone := src.Phone
		addr.Phone = &phone
	}

	if err := customerStore.CreateAddress(ctx, addr); err != nil {
		slog.WarnContext(ctx, "Failed to create shipping address", slog.Any("error", err))
		return nil
	}

	return &addrID
}
