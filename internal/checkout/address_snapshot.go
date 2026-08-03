package checkout

import (
	"encoding/json"
	"strings"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

const ShippingAddressNoteAttribute = "checkout_shipping_address"

// ShippingAddressSnapshot is persisted on the Shopify draft order so the webhook
// can recover the checkout address when Shopify omits shipping_address.
type ShippingAddressSnapshot struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company,omitempty"`
	Address1  string `json:"address1"`
	Address2  string `json:"address2,omitempty"`
	City      string `json:"city"`
	Province  string `json:"province"`
	Country   string `json:"country"`
	Zip       string `json:"zip"`
	Phone     string `json:"phone,omitempty"`
}

func ShippingAddressSnapshotFromRequest(req InitiateCheckoutRequest) *ShippingAddressSnapshot {
	address1 := strings.TrimSpace(req.Address1)
	city := strings.TrimSpace(req.City)
	zip := strings.TrimSpace(req.Zip)
	if address1 == "" || city == "" || zip == "" {
		return nil
	}

	country := strings.TrimSpace(req.Country)
	if country == "" {
		country = "US"
	}

	snap := &ShippingAddressSnapshot{
		FirstName: strings.TrimSpace(req.FirstName),
		LastName:  strings.TrimSpace(req.LastName),
		Company:   strings.TrimSpace(req.Company),
		Address1:  address1,
		City:      city,
		Province:  strings.TrimSpace(req.State),
		Country:   country,
		Zip:       zip,
		Phone:     strings.TrimSpace(req.Phone),
	}
	snap.Normalize()
	return snap
}

func ParseShippingAddressSnapshot(raw string) (*ShippingAddressSnapshot, error) {
	var snap ShippingAddressSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, err
	}
	snap.Normalize()
	return &snap, nil
}

func (s *ShippingAddressSnapshot) JSON() (string, error) {
	s.Normalize()
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *ShippingAddressSnapshot) IsValid() bool {
	if s == nil {
		return false
	}
	return strings.TrimSpace(s.Address1) != "" &&
		strings.TrimSpace(s.City) != "" &&
		strings.TrimSpace(s.Zip) != ""
}

func (s *ShippingAddressSnapshot) Normalize() {
	if s == nil {
		return
	}
	if strings.TrimSpace(s.Country) == "" {
		s.Country = "US"
	}
	if strings.TrimSpace(s.Province) == "" {
		s.Province = "N/A"
	}
}

func resolveBillingAddress(req InitiateCheckoutRequest, shipping *shopify.AddressInput) *shopify.AddressInput {
	if shipping == nil {
		return nil
	}
	billingEmpty := strings.TrimSpace(req.BillingAddress1) == "" &&
		strings.TrimSpace(req.BillingCity) == ""
	if req.SameAsShipping || billingEmpty {
		copied := *shipping
		return &copied
	}

	country := strings.TrimSpace(req.BillingCountry)
	if country == "" {
		country = shipping.Country
	}

	return &shopify.AddressInput{
		FirstName: req.BillingFirstName,
		LastName:  req.BillingLastName,
		Company:   req.BillingCompany,
		Address1:  req.BillingAddress1,
		City:      req.BillingCity,
		Province:  req.BillingState,
		Zip:       req.BillingZip,
		Country:   country,
		Phone:     req.BillingPhone,
	}
}
