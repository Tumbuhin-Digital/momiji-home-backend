package shopify

import "testing"

func TestNewShippingLineInputSetsPriceWithCurrency(t *testing.T) {
	line := NewShippingLineInput("UPS Ground", "17.50", "USD")
	if line == nil {
		t.Fatal("expected shipping line")
	}
	if line.Price != "17.50" {
		t.Fatalf("unexpected legacy price: %s", line.Price)
	}
	if line.PriceWithCurrency == nil {
		t.Fatal("expected priceWithCurrency")
	}
	if line.PriceWithCurrency.Amount != "17.50" || line.PriceWithCurrency.CurrencyCode != "USD" {
		t.Fatalf("unexpected priceWithCurrency: %+v", line.PriceWithCurrency)
	}
}
