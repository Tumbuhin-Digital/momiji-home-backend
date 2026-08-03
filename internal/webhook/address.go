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
		Company:   snap.Company,
		Address1:  snap.Address1,
		Address2:  snap.Address2,
		City:      snap.City,
		Province:  snap.Province,
		Country:   snap.Country,
		Zip:       snap.Zip,
		Phone:     snap.Phone,
	}
}

func createCustomerAddress(
	ctx context.Context,
	customerStore customer.CustomerStore,
	customerID string,
	src *ShopifyAddress,
	isDefault bool,
	label string,
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
		IsDefault:  isDefault,
	}
	if src.FirstName != "" {
		firstName := src.FirstName
		addr.FirstName = &firstName
	}
	if src.LastName != "" {
		lastName := src.LastName
		addr.LastName = &lastName
	}
	if strings.TrimSpace(src.Company) != "" {
		company := strings.TrimSpace(src.Company)
		addr.Company = &company
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
		slog.WarnContext(ctx, "Failed to create "+label+" address", slog.Any("error", err))
		return nil
	}

	return &addrID
}

func createCustomerShippingAddress(
	ctx context.Context,
	customerStore customer.CustomerStore,
	customerID string,
	src *ShopifyAddress,
) *string {
	return createCustomerAddress(ctx, customerStore, customerID, src, true, "shipping")
}

func createCustomerBillingAddress(
	ctx context.Context,
	customerStore customer.CustomerStore,
	customerID string,
	src *ShopifyAddress,
) *string {
	return createCustomerAddress(ctx, customerStore, customerID, src, false, "billing")
}

func addressesEqual(a, b *ShopifyAddress) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(a.Address1), strings.TrimSpace(b.Address1)) &&
		strings.EqualFold(strings.TrimSpace(a.City), strings.TrimSpace(b.City)) &&
		strings.EqualFold(strings.TrimSpace(a.Zip), strings.TrimSpace(b.Zip)) &&
		strings.EqualFold(strings.TrimSpace(a.Province), strings.TrimSpace(b.Province)) &&
		strings.EqualFold(strings.TrimSpace(a.Country), strings.TrimSpace(b.Country)) &&
		strings.EqualFold(strings.TrimSpace(a.Company), strings.TrimSpace(b.Company)) &&
		strings.EqualFold(strings.TrimSpace(a.FirstName), strings.TrimSpace(b.FirstName)) &&
		strings.EqualFold(strings.TrimSpace(a.LastName), strings.TrimSpace(b.LastName))
}
