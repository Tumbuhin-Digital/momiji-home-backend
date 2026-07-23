package shipping

import "testing"

func TestHumanizeShippingMethod(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"ups_ground", "UPS Ground"},
		{"UPS Ground", "UPS Ground"},
		{"fedex_ground", "FedEx Ground"},
		{"", ""},
		{"custom_carrier_express", "Custom Carrier Express"},
	}
	for _, tt := range tests {
		if got := HumanizeShippingMethod(tt.in); got != tt.want {
			t.Fatalf("HumanizeShippingMethod(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCustomerShippingLineTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "Shipping & delivery"},
		{"ups_ground", "Shipping & delivery (UPS Ground)"},
		{"UPS Ground", "Shipping & delivery (UPS Ground)"},
	}
	for _, tt := range tests {
		if got := CustomerShippingLineTitle(tt.in); got != tt.want {
			t.Fatalf("CustomerShippingLineTitle(%q)=%q, want %q", tt.in, got, tt.want)
		}
	}
}
