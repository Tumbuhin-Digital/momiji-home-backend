package order

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

const testUpfrontShippingLine = "gid://shopify/CalculatedLineItem/12"

// upfrontShippingCalcLine is the checkout's "Shipping (Pre-Order) - 50%" line.
func upfrontShippingCalcLine(id string, amount float64) shopify.CalculatedLineItem {
	return shopify.CalculatedLineItem{
		ID:                  id,
		Title:               "Shipping (Pre-Order) - 50%",
		Quantity:            1,
		CustomAttributes:    []shopify.AttributeInput{{Key: "charge_type", Value: preorderShippingDepositChargeType}},
		OriginalUnitPrice:   amount,
		DiscountedUnitPrice: amount,
	}
}

// The PRD example with the upfront line folded: it is removed ($83.44 credited) and the
// group's shipping is one full $170 line, so the customer still owes $611.56.
func TestPlanBalanceEdit_FoldsTheUpfrontShippingLine(t *testing.T) {
	calc := []shopify.CalculatedLineItem{
		depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 3, 175),
		upfrontShippingCalcLine(testUpfrontShippingLine, 83.44),
	}

	plan, err := planBalanceEdit([]balanceEditLine{shelfLine(3)}, calc, 86.56, "Shipping & delivery (UPS Ground)", 83.44)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.ShippingDeposit == nil || plan.ShippingDeposit.CalculatedLineID != testUpfrontShippingLine || plan.ShippingDeposit.Amount != 83.44 {
		t.Fatalf("expected the upfront line removed, got %+v", plan.ShippingDeposit)
	}
	if plan.Shipping == nil || plan.Shipping.Amount != 170 || plan.Shipping.Title != "Shipping & delivery (UPS Ground)" {
		t.Fatalf("expected one full $170 shipping line, got %+v", plan.Shipping)
	}
	if plan.OutstandingIncrease != 611.56 {
		t.Fatalf("folding must not change what is owed; expected 611.56, got %v", plan.OutstandingIncrease)
	}
}

// Final shipping equal to the prepayment: nothing more to pay for shipping, but the full
// line still shows it.
func TestPlanBalanceEdit_FoldWithNothingLeftToPay(t *testing.T) {
	calc := []shopify.CalculatedLineItem{
		depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 3, 175),
		upfrontShippingCalcLine(testUpfrontShippingLine, 83.44),
	}

	plan, err := planBalanceEdit([]balanceEditLine{shelfLine(3)}, calc, 0, "Shipping & delivery", 83.44)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.ShippingDeposit == nil || plan.Shipping == nil || plan.Shipping.Amount != 83.44 || plan.OutstandingIncrease != 525 {
		t.Fatalf("expected an $83.44 line and $525 owed, got %+v / %+v / %v", plan.ShippingDeposit, plan.Shipping, plan.OutstandingIncrease)
	}
}

func TestPlanBalanceEdit_KeepsTheUpfrontShippingLine(t *testing.T) {
	deposit := depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 3, 175)
	cases := []struct {
		name string
		calc []shopify.CalculatedLineItem
		fold float64
	}{
		{"prepayment shared with other groups", []shopify.CalculatedLineItem{deposit, upfrontShippingCalcLine(testUpfrontShippingLine, 83.44)}, 0},
		{"line holds a different amount", []shopify.CalculatedLineItem{deposit, upfrontShippingCalcLine(testUpfrontShippingLine, 50)}, 83.44},
		{"no upfront line", []shopify.CalculatedLineItem{deposit}, 83.44},
		{"two upfront lines", []shopify.CalculatedLineItem{deposit, upfrontShippingCalcLine(testUpfrontShippingLine, 83.44), upfrontShippingCalcLine("gid://shopify/CalculatedLineItem/13", 83.44)}, 83.44},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planBalanceEdit([]balanceEditLine{shelfLine(3)}, tc.calc, 86.56, "Shipping & delivery", tc.fold)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if plan.ShippingDeposit != nil || plan.Shipping == nil || plan.Shipping.Amount != 86.56 || plan.OutstandingIncrease != 611.56 {
				t.Fatalf("expected the upfront line kept and $86.56 shipping, got %+v / %+v / %v", plan.ShippingDeposit, plan.Shipping, plan.OutstandingIncrease)
			}
		})
	}
}

func TestFoldableShippingDeposit(t *testing.T) {
	shipments := []PreorderShipment{{ID: "ship-1", PrepaidShipping: 83.44}, {ID: "ship-2"}}
	if got := foldableShippingDeposit(shipments, 83.44); got != 83.44 {
		t.Fatalf("a group using the whole prepayment folds it, got %v", got)
	}
	if got := foldableShippingDeposit(shipments, 40); got != 0 {
		t.Fatalf("a group using part of the prepayment must keep the line, got %v", got)
	}
	if got := foldableShippingDeposit([]PreorderShipment{{ID: "ship-1"}}, 0); got != 0 {
		t.Fatalf("no prepayment, nothing to fold, got %v", got)
	}
}

func TestRequestSecondPaymentByOrderEdit_FoldsTheUpfrontShippingLine(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	editor.calc.LineItems = append(editor.calc.LineItems, upfrontShippingCalcLine(testUpfrontShippingLine, 83.44))

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.lineQty[testUpfrontShippingLine] != 0 {
		t.Fatal("expected the upfront shipping line removed")
	}
	if got := editor.shipping["gid://shopify/CalculatedShippingLine/new"]; got != 170 {
		t.Fatalf("expected one full $170 shipping line, got %v", got)
	}
	if strings.Contains(editor.shippingTitle, "Remaining") {
		t.Fatalf("the shipping line must read like a regular order's, got %q", editor.shippingTitle)
	}
	if store.completed == nil || store.completed.ExpectedOutstanding != 611.56 || store.shipment.BalanceShippingDepositFolded != 83.44 {
		t.Fatalf("expected $611.56 owed and $83.44 folded, got %+v / %v", store.completed, store.shipment.BalanceShippingDepositFolded)
	}
}

// A group that uses only part of the prepayment leaves the upfront line for the others.
func TestRequestSecondPaymentByOrderEdit_KeepsASharedUpfrontShippingLine(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	editor.calc.LineItems = append(editor.calc.LineItems, upfrontShippingCalcLine(testUpfrontShippingLine, 83.44))
	final := 50.0
	store.shipment.FinalShippingPrice = &final

	if _, err := svc.RequestSecondPayment(context.Background(), "", "order-1", RequestSecondPaymentRequest{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if editor.lineQty[testUpfrontShippingLine] != 1 || store.shipment.BalanceShippingDepositFolded != 0 {
		t.Fatalf("expected the upfront line kept, got qty %d folded %v", editor.lineQty[testUpfrontShippingLine], store.shipment.BalanceShippingDepositFolded)
	}
	if _, added := editor.shipping["gid://shopify/CalculatedShippingLine/new"]; added {
		t.Fatal("the prepayment covers this group's shipping; no shipping line expected")
	}
	if store.completed == nil || store.completed.ExpectedOutstanding != 525 {
		t.Fatalf("expected $525 owed, got %+v", store.completed)
	}
}

// Re-pricing a folded group keeps the folded prepayment inside the new full line.
func TestUpdatePreorderShipping_RepricesAFoldedShippingLine(t *testing.T) {
	store, editor, svc := balanceEditFixture()
	method, sent, url, lineID := BalanceMethodOrderEdit, time.Now(), "https://pay.example/1325", "gid://shopify/ShippingLine/31"
	expected := 611.56
	store.shipment.BalanceMethod = &method
	store.shipment.InvoiceSentAt = &sent
	store.shipment.InvoiceURL = &url
	store.shipment.ShopifyShippingLineID = &lineID
	store.shipment.BalanceExpectedOutstanding = &expected
	store.shipment.PrepaidApplied = 83.44
	store.shipment.BalanceShippingDepositFolded = 83.44
	editor.before = &shopify.OrderBalanceState{
		Outstanding:   611.56,
		ShippingLines: []shopify.OrderShippingLineState{{ID: lineID, Title: testShippingTitle, Price: 170}},
	}
	editor.calc = &shopify.CalculatedOrder{
		ID:            "gid://shopify/CalculatedOrder/2",
		Outstanding:   611.56,
		ShippingLines: []shopify.CalculatedShippingLine{{ID: "gid://shopify/CalculatedShippingLine/31", Title: testShippingTitle, Price: 170}},
	}
	editor.after = &shopify.OrderBalanceState{
		Outstanding:          621.56,
		PaymentCollectionURL: url,
		ShippingLines:        []shopify.OrderShippingLineState{{ID: "gid://shopify/ShippingLine/32", Title: testShippingTitle, Price: 180}},
	}

	if _, err := svc.UpdatePreorderShipping(context.Background(), "", "order-1", UpdatePreorderShippingRequest{FinalShippingPrice: 180}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := editor.shipping["gid://shopify/CalculatedShippingLine/new"]; got != 180 {
		t.Fatalf("expected the new full line at $180, got %v", got)
	}
	// 611.56 - 170 + 180
	if store.repriced == nil || store.repriced.expected != 621.56 {
		t.Fatalf("expected outstanding 621.56, got %+v", store.repriced)
	}
}
