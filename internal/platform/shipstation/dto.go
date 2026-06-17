package shipstation

// --- Rate Request ---

type RateRequest struct {
	RateOptions RateOptions `json:"rate_options"`
	Shipment    Shipment    `json:"shipment"`
}

type RateOptions struct {
	CarrierIDs []string `json:"carrier_ids"`
}

type Shipment struct {
	ValidateAddress string    `json:"validate_address"` // "no_validation"
	ShipFrom        Address   `json:"ship_from"`
	ShipTo          Address   `json:"ship_to"`
	Packages        []Package `json:"packages"`
}

type Address struct {
	Name          string `json:"name"`
	Phone         string `json:"phone"`
	AddressLine1  string `json:"address_line1"`
	CityLocality  string `json:"city_locality"`
	StateProvince string `json:"state_province"`
	PostalCode    string `json:"postal_code"`
	CountryCode   string `json:"country_code"` // "US"
}

type Package struct {
	Weight     Weight      `json:"weight"`
	Dimensions *Dimensions `json:"dimensions,omitempty"`
}

type Weight struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"` // "pound", "ounce", "kilogram", "gram"
}

type Dimensions struct {
	Unit   string  `json:"unit"` // "inch", "centimeter"
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// --- Rate Response ---

type RateResponseWrapper struct {
	RateResponse RateResponse `json:"rate_response"`
}

type RateResponse struct {
	Rates        []Rate        `json:"rates"`
	InvalidRates []interface{} `json:"invalid_rates,omitempty"`
	Errors       []interface{} `json:"errors,omitempty"`
}

type Rate struct {
	RateID              string `json:"rate_id"`
	CarrierID           string `json:"carrier_id"`
	ServiceCode         string `json:"service_code"`
	CarrierFriendlyName string `json:"carrier_friendly_name"`
	ServiceType         string `json:"service_type"`
	ShippingAmount      Money  `json:"shipping_amount"`
	DeliveryDays        *int   `json:"delivery_days"`
}

type Money struct {
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
}

// --- Carrier ---

type Carrier struct {
	CarrierID    string `json:"carrier_id"`
	FriendlyName string `json:"friendly_name"`
	Code         string `json:"code"`
}
