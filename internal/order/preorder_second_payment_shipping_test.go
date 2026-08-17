package order

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
)

// capturingInvoicer records the GroupInvoiceOptions handed to the settlement invoice.
type capturingInvoicer struct {
	captured preorder.GroupInvoiceOptions
}

func (c *capturingInvoicer) CreateGroupSecondPaymentInvoice(_ context.Context, opts preorder.GroupInvoiceOptions) (*preorder.GroupInvoiceResult, error) {
	c.captured = opts
	return &preorder.GroupInvoiceResult{DraftOrderID: "draft-1", InvoiceURL: "https://invoice.example/1"}, nil
}

func (c *capturingInvoicer) ListSettlements(context.Context, preorder.SettlementFilter) ([]preorder.PreorderGroupResponse, int64, error) {
	return nil, 0, nil
}
func (c *capturingInvoicer) GetSettlement(context.Context, string) (*preorder.SettlementResponse, error) {
	return nil, nil
}
func (c *capturingInvoicer) InvoiceSettlements(context.Context, []string) ([]preorder.SettlementResponse, error) {
	return nil, nil
}
func (c *capturingInvoicer) InvoiceSettlementsWithShipping(context.Context, []string, preorder.InvoiceOptions) ([]preorder.SettlementResponse, error) {
	return nil, nil
}
func (c *capturingInvoicer) MarkSettlementsPaid(context.Context, []string) ([]preorder.SettlementResponse, error) {
	return nil, nil
}
func (c *capturingInvoicer) ProcessReminders(context.Context) error { return nil }
func (c *capturingInvoicer) ExportPreordersToExcel(context.Context, preorder.SettlementFilter) ([]byte, error) {
	return nil, nil
}
func (c *capturingInvoicer) CascadeSettlementPayment(context.Context, string) error { return nil }

var _ preorder.PreorderService = (*capturingInvoicer)(nil)

func secondPaymentFixture(finalShipping, prepaidShipping float64) (*updateShippingWarehouseStore, *capturingInvoicer, *service) {
	lineID := "line-1"
	balance := 745.00
	title := "Rattan Chair"

	store := &updateShippingWarehouseStore{
		order: &Order{
			ID:              "order-1",
			CustomerID:      "cust-1",
			Customer:        &customer.Customer{Email: "buyer@example.com"},
			ShippingAddress: &customer.Address{Address1: "90 Dayton Ave", City: "Passaic", Province: "NJ", Zip: "07055", Country: "US"},
			Items: []OrderItem{
				{ID: lineID, Type: "pre_order", ShopifyVariantID: "var-1", Quantity: 2, FulfillmentStep: 2, BalanceDue: &balance, Title: &title},
			},
		},
		shipment: &PreorderShipment{
			ID:                 "ship-1",
			OrderID:            "order-1",
			FinalShippingPrice: &finalShipping,
			PrepaidShipping:    prepaidShipping,
			WarehouseOrigin:    "west",
		},
	}

	invoicer := &capturingInvoicer{}
	return store, invoicer, &service{store: store, preorderService: invoicer}
}

func TestRequestSecondPayment_BillsOnlyTheUnpaidShippingHalf(t *testing.T) {
	store, invoicer, svc := secondPaymentFixture(456.64, 228.32)

	if err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := invoicer.captured.ShippingPrice; got != 228.32 {
		t.Fatalf("expected remaining shipping 228.32, got %v", got)
	}
	if got := invoicer.captured.ShippingPrepaid; got != 228.32 {
		t.Fatalf("expected prepaid 228.32 passed through, got %v", got)
	}
	// The two shipping charges must reconstruct the admin-set final price exactly.
	if sum := invoicer.captured.ShippingPrice + store.shipment.PrepaidShipping; sum != 456.64 {
		t.Fatalf("shipping halves sum to %v, want 456.64", sum)
	}
}

// Orders placed before the split scheme carry prepaid 0 and must still be billed in full.
func TestRequestSecondPayment_LegacyOrderWithoutPrepaidBillsFullShipping(t *testing.T) {
	_, invoicer, svc := secondPaymentFixture(456.64, 0)

	if err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := invoicer.captured.ShippingPrice; got != 456.64 {
		t.Fatalf("expected full shipping 456.64, got %v", got)
	}
	if got := invoicer.captured.ShippingPrepaid; got != 0 {
		t.Fatalf("expected prepaid 0, got %v", got)
	}
}

// A final price below what was already collected must never produce a negative charge.
func TestRequestSecondPayment_FinalBelowPrepaidClampsToZero(t *testing.T) {
	_, invoicer, svc := secondPaymentFixture(100, 228.32)

	if err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := invoicer.captured.ShippingPrice; got != 0 {
		t.Fatalf("expected shipping clamped to 0, got %v", got)
	}
}
