package shipping_test

import (
	"context"
	"math"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shipstation"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
)

type mockShipStationClient struct {
	lastReq shipstation.RateRequest
	rates   []shipstation.Rate
	err     error
}

func (m *mockShipStationClient) GetRates(_ context.Context, req shipstation.RateRequest) ([]shipstation.Rate, error) {
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	return m.rates, nil
}

func (m *mockShipStationClient) ListCarriers(context.Context) ([]shipstation.Carrier, error) {
	return nil, nil
}

func (m *mockShipStationClient) TrackShipment(context.Context, string, string) (*shipstation.TrackingResponse, error) {
	return nil, nil
}

func TestBuildPackages_ConvertsKgToLbOnce(t *testing.T) {
	ctx := context.Background()
	pkgs := shipping.BuildPackages(ctx, []shipping.PackableUnit{
		{WeightKg: 1.0, BoxCount: 1},
	})
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	want := units.KgToLb(1.0)
	if math.Abs(pkgs[0].Weight.Value-want) > 1e-9 {
		t.Fatalf("expected %f lb, got %f", want, pkgs[0].Weight.Value)
	}
	if pkgs[0].Weight.Unit != "pound" {
		t.Fatalf("expected pound unit, got %q", pkgs[0].Weight.Unit)
	}
}

func TestBuildPackages_ConvertsDimensionsToInches(t *testing.T) {
	ctx := context.Background()
	pkgs := shipping.BuildPackages(ctx, []shipping.PackableUnit{
		{
			WeightKg: 2,
			DepthCm:  100,
			WidthCm:  50,
			HeightCm: 30,
			BoxCount: 1,
		},
	})
	if len(pkgs) != 1 {
		t.Fatalf("expected 1 package, got %d", len(pkgs))
	}
	if pkgs[0].Dimensions == nil {
		t.Fatal("expected dimensions")
	}
	d := pkgs[0].Dimensions
	if d.Unit != "inch" {
		t.Fatalf("expected inch unit, got %q", d.Unit)
	}
	if d.Length != units.CmToIn(100) {
		t.Fatalf("length: got %f want %f", d.Length, units.CmToIn(100))
	}
	if d.Width != units.CmToIn(50) {
		t.Fatalf("width: got %f want %f", d.Width, units.CmToIn(50))
	}
	if d.Height != units.CmToIn(30) {
		t.Fatalf("height: got %f want %f", d.Height, units.CmToIn(30))
	}
}

func TestBuildPackages_MultiBox(t *testing.T) {
	ctx := context.Background()
	pkgs := shipping.BuildPackages(ctx, []shipping.PackableUnit{
		{WeightKg: 5, BoxCount: 3},
	})
	if len(pkgs) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(pkgs))
	}
	for i, pkg := range pkgs {
		if pkg.Weight.Unit != "pound" {
			t.Fatalf("package %d: expected pound", i)
		}
		if math.Abs(pkg.Weight.Value-units.KgToLb(5)) > 1e-9 {
			t.Fatalf("package %d: wrong weight", i)
		}
	}
}

func TestPackableUnitFromCartItem_KilogramsPassthrough(t *testing.T) {
	unit := shipping.PackableUnitFromCartItem(3.5, "KILOGRAMS", 10, 20, 30, 2)
	if unit.WeightKg != 3.5 {
		t.Fatalf("expected 3.5 kg, got %f", unit.WeightKg)
	}
	if unit.BoxCount != 2 {
		t.Fatalf("expected box count 2, got %d", unit.BoxCount)
	}
}

func TestCalculateGroundRate_SumsAllAmountComponents(t *testing.T) {
	client := &mockShipStationClient{
		rates: []shipstation.Rate{{
			ServiceCode: "ups_ground",
			ShippingAmount: shipstation.Money{
				Currency: "USD",
				Amount:   69.01,
			},
			ConfirmationAmount: shipstation.Money{Currency: "USD", Amount: 0},
			InsuranceAmount:    shipstation.Money{Currency: "USD", Amount: 0},
			OtherAmount:        shipstation.Money{Currency: "USD", Amount: 21.20},
		}},
	}

	amount, currency, err := shipping.CalculateGroundRate(
		context.Background(),
		client,
		shipping.ShipFromAddress{
			Name: "Momiji Home", Phone: "555-123-4567", Address1: "100 Momiji Way",
			City: "Passaic", State: "NJ", Zip: "07055", Country: "US",
		},
		[]string{"se-1730633"},
		"ups_ground",
		shipping.ShipToAddress{
			Name: "Customer", Phone: "555-555-5555", Address1: "123 Main St",
			City: "Los Angeles", State: "CA", Zip: "90001", Country: "US",
		},
		[]shipstation.Package{{
			Weight: shipstation.Weight{Value: 102, Unit: "pound"},
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if currency != "USD" {
		t.Fatalf("expected USD, got %q", currency)
	}
	if math.Abs(amount-90.21) > 1e-9 {
		t.Fatalf("expected total 90.21, got %f", amount)
	}
	if client.lastReq.Shipment.Confirmation != "none" {
		t.Fatalf("expected confirmation none (UI Online), got %q", client.lastReq.Shipment.Confirmation)
	}
	if client.lastReq.Shipment.ShipTo.AddressResidentialIndicator != "unknown" {
		t.Fatalf("expected residential indicator unknown, got %q", client.lastReq.Shipment.ShipTo.AddressResidentialIndicator)
	}
}

func TestCalculateGroundRate_IncludesConfirmationAmount(t *testing.T) {
	client := &mockShipStationClient{
		rates: []shipstation.Rate{{
			ServiceCode:        "ups_ground",
			ShippingAmount:     shipstation.Money{Currency: "USD", Amount: 69.01},
			ConfirmationAmount: shipstation.Money{Currency: "USD", Amount: 7.50},
			OtherAmount:        shipstation.Money{Currency: "USD", Amount: 21.20},
		}},
	}

	amount, _, err := shipping.CalculateGroundRate(
		context.Background(),
		client,
		shipping.ShipFromAddress{Zip: "07055", State: "NJ", Country: "US"},
		[]string{"se-1730633"},
		"ups_ground",
		shipping.ShipToAddress{Zip: "90001", State: "CA", Country: "US"},
		[]shipstation.Package{{Weight: shipstation.Weight{Value: 102, Unit: "pound"}}},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(amount-97.71) > 1e-9 {
		t.Fatalf("expected total 97.71, got %f", amount)
	}
}
