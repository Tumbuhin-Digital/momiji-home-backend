package order

import (
	"context"
	"testing"
)

func TestRequestSecondPayment_InvoiceLinesCarryVariantSKU(t *testing.T) {
	_, invoicer, svc := secondPaymentFixture(456.64, 228.32)

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(invoicer.captured.Lines) != 1 {
		t.Fatalf("expected 1 invoice line, got %d", len(invoicer.captured.Lines))
	}
	if got := invoicer.captured.Lines[0].SKU; got != "MU-RATTAN-01" {
		t.Fatalf("expected SKU MU-RATTAN-01, got %q", got)
	}
}

func TestRequestSecondPayment_PassesDepositOrderNumber(t *testing.T) {
	store, invoicer, svc := secondPaymentFixture(456.64, 228.32)
	store.order.OrderNumber = "ORD-1323"

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := invoicer.captured.DepositOrderNumber; got != "1323" {
		t.Fatalf("expected deposit order number 1323, got %q", got)
	}
}

func TestShopifyOrderNumber(t *testing.T) {
	cases := map[string]string{
		"ORD-1323":     "1323",
		" ORD-42 ":     "42",
		"ORD-3f9a1c2e": "", // older orders numbered from a UUID, not Shopify
		"":             "",
	}
	for in, want := range cases {
		if got := shopifyOrderNumber(in); got != want {
			t.Errorf("shopifyOrderNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
