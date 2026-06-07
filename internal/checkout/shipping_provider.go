package checkout

import (
	"context"
)

type ShippingProvider interface {
	GetRates(ctx context.Context, req ShippingRateRequest) ([]ShippingRate, error)
}

type ShippingRateRequest struct {
	OriginZip      string
	DestinationZip string
	WeightKg       float64
	WidthCm        float64
	HeightCm       float64
	DepthCm        float64
}

type ShippingRate struct {
	ProviderID       string
	Label            string
	EstimatedArrival string
	Cost             float64
	Currency         string
}

type MockShippingProvider struct{}

func (m *MockShippingProvider) GetRates(ctx context.Context, req ShippingRateRequest) ([]ShippingRate, error) {
	// Static mock rates for now until real provider is integrated
	return []ShippingRate{
		{ProviderID: "ground", Label: "Ground", EstimatedArrival: "5-7 Business Days", Cost: 20.00, Currency: "USD"},
		{ProviderID: "expedited", Label: "Expedited", EstimatedArrival: "2-3 Business Days", Cost: 35.00, Currency: "USD"},
		{ProviderID: "next_business_day", Label: "Next Business Day", EstimatedArrival: "1 Business Day", Cost: 60.00, Currency: "USD"},
	}, nil
}
