package order

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/customer"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorder"
)

// capturingInvoicer records the GroupInvoiceOptions handed to the settlement invoice.
type capturingInvoicer struct {
	captured preorder.GroupInvoiceOptions

	updated        preorder.GroupInvoiceOptions
	updatedDraftID string
	updateCalls    int
	updateErr      error

	emailed     preorder.GroupInvoiceOptions
	emailedLink string
}

func (c *capturingInvoicer) CreateGroupSecondPaymentInvoice(_ context.Context, opts preorder.GroupInvoiceOptions) (*preorder.GroupInvoiceResult, error) {
	c.captured = opts
	return &preorder.GroupInvoiceResult{DraftOrderID: "draft-1", InvoiceURL: "https://invoice.example/1"}, nil
}

func (c *capturingInvoicer) UpdateGroupSecondPaymentInvoice(_ context.Context, draftOrderID string, opts preorder.GroupInvoiceOptions) (*preorder.GroupInvoiceResult, error) {
	if c.updateErr != nil {
		return nil, c.updateErr
	}
	c.updatedDraftID = draftOrderID
	c.updated = opts
	c.updateCalls++
	return &preorder.GroupInvoiceResult{DraftOrderID: draftOrderID, InvoiceURL: "https://invoice.example/1"}, nil
}

func (c *capturingInvoicer) SendGroupSecondPaymentInvoiceEmail(_ context.Context, opts preorder.GroupInvoiceOptions, link string) error {
	c.emailed = opts
	c.emailedLink = link
	return nil
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

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
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

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
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

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := invoicer.captured.ShippingPrice; got != 0 {
		t.Fatalf("expected shipping clamped to 0, got %v", got)
	}
}

// invoicedFixture is a group whose payment link was created (step 3) but not paid yet.
func invoicedFixture(finalShipping, prepaidShipping float64) (*updateShippingWarehouseStore, *capturingInvoicer, *service) {
	store, invoicer, svc := secondPaymentFixture(finalShipping, prepaidShipping)
	sentAt := time.Now()
	draftID := "draft-1"
	url := "https://invoice.example/1"
	store.shipment.InvoiceSentAt = &sentAt
	store.shipment.ShopifyDraftOrderID = &draftID
	store.shipment.InvoiceURL = &url
	store.shipment.PrepaidApplied = AllocatePrepaidShipping([]PreorderShipment{*store.shipment})[store.shipment.ID]
	store.order.Items[0].FulfillmentStep = preOrderStepSecondPayment
	return store, invoicer, svc
}

func TestUpdatePreorderShipping_UnpaidInvoiceIsRepricedOnTheSameDraft(t *testing.T) {
	store, invoicer, svc := invoicedFixture(456.64, 228.32)

	_, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{
		FinalShippingPrice: 500,
		ShippingNotes:      "Freight re-quote",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invoicer.updateCalls != 1 || invoicer.updatedDraftID != "draft-1" {
		t.Fatalf("expected one update of draft-1, got %d call(s) on %q", invoicer.updateCalls, invoicer.updatedDraftID)
	}
	if got := invoicer.updated.ShippingPrice; got != 271.68 {
		t.Fatalf("expected invoice shipping 500 - 228.32 = 271.68, got %v", got)
	}
	if got := invoicer.updated.ShippingNotes; got != "Freight re-quote" {
		t.Fatalf("expected new notes on the invoice, got %q", got)
	}
	if len(invoicer.updated.Lines) != 1 || invoicer.updated.Lines[0].Amount != 745 {
		t.Fatalf("expected the item balance to stay on the invoice, got %+v", invoicer.updated.Lines)
	}
	if store.shipment.FinalShippingPrice == nil || *store.shipment.FinalShippingPrice != 500 {
		t.Fatalf("expected saved final price 500, got %v", store.shipment.FinalShippingPrice)
	}
	if store.shipment.PrepaidApplied != 228.32 {
		t.Fatalf("expected prepaid share 228.32 kept, got %v", store.shipment.PrepaidApplied)
	}
	if store.order.Items[0].FulfillmentStep != preOrderStepSecondPayment {
		t.Fatalf("re-pricing must not move the item off step 3, got %d", store.order.Items[0].FulfillmentStep)
	}
}

// Resending after a re-price must email the new figures and the same link.
func TestResendSecondPaymentInvoice_AfterRepriceUsesNewAmounts(t *testing.T) {
	_, invoicer, svc := invoicedFixture(456.64, 228.32)

	if _, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{
		FinalShippingPrice: 100,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := svc.ResendSecondPaymentInvoice(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected resend error: %v", err)
	}

	if invoicer.emailedLink != "https://invoice.example/1" {
		t.Fatalf("expected the existing payment link, got %q", invoicer.emailedLink)
	}
	if got := invoicer.emailed.ShippingPrice; got != 0 {
		t.Fatalf("price below the prepayment should bill no shipping, got %v", got)
	}
	if got := invoicer.emailed.ShippingPrepaid; got != 100 {
		t.Fatalf("expected the deduction capped at the new 100 price, got %v", got)
	}
}

func TestUpdatePreorderShipping_PaidInvoiceIsRefused(t *testing.T) {
	store, invoicer, svc := invoicedFixture(456.64, 228.32)
	paidAt := time.Now()
	store.shipment.InvoicePaidAt = &paidAt

	_, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{
		FinalShippingPrice: 500,
	})
	if err != errSecondPaymentAlreadyPaid {
		t.Fatalf("expected already-paid error, got %v", err)
	}
	if invoicer.updateCalls != 0 {
		t.Fatal("a paid invoice must not be touched")
	}
	if *store.shipment.FinalShippingPrice != 456.64 {
		t.Fatalf("price must stay 456.64, got %v", *store.shipment.FinalShippingPrice)
	}
}

// If Shopify refuses (e.g. the customer paid a moment ago), our record must not change.
func TestUpdatePreorderShipping_ShopifyFailureSavesNothing(t *testing.T) {
	store, invoicer, svc := invoicedFixture(456.64, 228.32)
	invoicer.updateErr = errors.New("draft completed")

	if _, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{
		FinalShippingPrice: 500,
	}); err == nil {
		t.Fatal("expected the Shopify error to be returned")
	}
	if *store.shipment.FinalShippingPrice != 456.64 {
		t.Fatalf("price must stay 456.64 when Shopify fails, got %v", *store.shipment.FinalShippingPrice)
	}
}

// A rejected packing plan must be caught before Shopify is touched, or the invoice would
// carry a price that was never saved.
func TestUpdatePreorderShipping_InvalidPackingLeavesInvoiceAlone(t *testing.T) {
	store, invoicer, svc := invoicedFixture(456.64, 228.32)

	if _, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{
		FinalShippingPrice: 500,
		Packing:            []PackingItemDTO{{LineItemID: "not-on-order", BoxCount: 1}},
	}); err == nil {
		t.Fatal("expected the packing plan to be rejected")
	}
	if invoicer.updateCalls != 0 {
		t.Fatal("the Shopify invoice must not change when the request is invalid")
	}
	if *store.shipment.FinalShippingPrice != 456.64 {
		t.Fatalf("price must stay 456.64, got %v", *store.shipment.FinalShippingPrice)
	}
}

// Invoices from before per-group drafts were tracked have nothing to re-price.
func TestUpdatePreorderShipping_InvoiceWithoutDraftIsRefused(t *testing.T) {
	store, invoicer, svc := invoicedFixture(456.64, 228.32)
	store.shipment.ShopifyDraftOrderID = nil

	if _, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{
		FinalShippingPrice: 500,
	}); err == nil {
		t.Fatal("expected a conflict for an invoice without a draft")
	}
	if invoicer.updateCalls != 0 || *store.shipment.FinalShippingPrice != 456.64 {
		t.Fatal("nothing may change when the invoice cannot be re-priced")
	}
}
