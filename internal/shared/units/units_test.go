package units_test

import (
	"math"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/units"
)

func TestKgToLb_KnownValue(t *testing.T) {
	got := units.KgToLb(1)
	if math.Abs(got-units.KgToLbFactor) > 1e-9 {
		t.Fatalf("expected %f, got %f", units.KgToLbFactor, got)
	}
}

func TestCmToIn_KnownValue(t *testing.T) {
	got := units.CmToIn(100)
	if got != 39.37 {
		t.Fatalf("expected 39.37, got %f", got)
	}
}

func TestInToCm_KnownValue(t *testing.T) {
	got := units.InToCm(39.37)
	if got != 100.0 {
		t.Fatalf("expected 100.0, got %f", got)
	}
}

func TestLbKgRoundTrip(t *testing.T) {
	original := 5.0
	roundTrip := units.LbToKg(units.KgToLb(original))
	if math.Abs(roundTrip-original) > 1e-4 {
		t.Fatalf("round trip failed: got %f want %f", roundTrip, original)
	}
}

func TestShopifyWeightToKg(t *testing.T) {
	tests := []struct {
		value float64
		unit  string
		want  float64
		ok    bool
	}{
		{1, "KILOGRAMS", 1, true},
		{1000, "GRAMS", 1, true},
		{1, "POUNDS", units.LbToKg(1), true},
		{16, "OUNCES", units.OzToKg(16), true},
		{5, "STONE", 5, false},
	}
	for _, tc := range tests {
		got, ok := units.ShopifyWeightToKg(tc.value, tc.unit)
		if ok != tc.ok {
			t.Fatalf("unit %q recognized=%v want %v", tc.unit, ok, tc.ok)
		}
		if math.Abs(got-tc.want) > 1e-6 {
			t.Fatalf("unit %q got %f want %f", tc.unit, got, tc.want)
		}
	}
}

func TestCartWeightToKg(t *testing.T) {
	tests := []struct {
		value float64
		unit  string
		want  float64
		ok    bool
	}{
		{2.5, "KILOGRAMS", 2.5, true},
		{500, "GRAMS", 0.5, true},
		{10, "POUNDS", units.LbToKg(10), true},
		{8, "OUNCES", units.OzToKg(8), true},
		{3, "UNKNOWN", 3, false},
	}
	for _, tc := range tests {
		got, ok := units.CartWeightToKg(tc.value, tc.unit)
		if ok != tc.ok {
			t.Fatalf("unit %q recognized=%v want %v", tc.unit, ok, tc.ok)
		}
		if math.Abs(got-tc.want) > 1e-6 {
			t.Fatalf("unit %q got %f want %f", tc.unit, got, tc.want)
		}
	}
}
