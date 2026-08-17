package shipping_test

import (
	"math"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shipping"
)

func TestSplitHalf(t *testing.T) {
	tests := []struct {
		name          string
		total         float64
		wantUpfront   float64
		wantRemaining float64
	}{
		{name: "even amount", total: 456.64, wantUpfront: 228.32, wantRemaining: 228.32},
		{name: "odd cent rounds up front", total: 100.01, wantUpfront: 50.01, wantRemaining: 50.00},
		{name: "free shipping", total: 0, wantUpfront: 0, wantRemaining: 0},
		{name: "negative treated as zero", total: -5, wantUpfront: 0, wantRemaining: 0},
		{name: "single cent", total: 0.01, wantUpfront: 0.01, wantRemaining: 0},
		{name: "rounds total before splitting", total: 33.335, wantUpfront: 16.67, wantRemaining: 16.67},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upfront, remaining := shipping.SplitHalf(tt.total)
			if math.Abs(upfront-tt.wantUpfront) > 1e-9 {
				t.Errorf("upfront = %v, want %v", upfront, tt.wantUpfront)
			}
			if math.Abs(remaining-tt.wantRemaining) > 1e-9 {
				t.Errorf("remaining = %v, want %v", remaining, tt.wantRemaining)
			}
		})
	}
}

// The two halves must reconstruct the total exactly — a stray cent here would mean
// the customer is over- or under-billed across the two payments.
func TestSplitHalf_HalvesAlwaysSumToTotal(t *testing.T) {
	for cents := 1; cents <= 200000; cents++ {
		total := float64(cents) / 100
		upfront, remaining := shipping.SplitHalf(total)
		sum := math.Round((upfront+remaining)*100) / 100
		if math.Abs(sum-total) > 1e-9 {
			t.Fatalf("total %.2f split into %.2f + %.2f = %.2f", total, upfront, remaining, sum)
		}
	}
}
