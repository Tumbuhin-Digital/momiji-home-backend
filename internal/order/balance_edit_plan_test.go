package order

import (
	"errors"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

const (
	shelfVariant = "gid://shopify/ProductVariant/501"
	toteVariant  = "gid://shopify/ProductVariant/502"
)

func depositCalcLine(id, variant string, qty int, unit float64) shopify.CalculatedLineItem {
	return shopify.CalculatedLineItem{
		ID:       id,
		Title:    "After the Rain Shelf (Deposit 50%)",
		Quantity: qty,
		CustomAttributes: []shopify.AttributeInput{
			{Key: "type", Value: "preorder_dp"},
			{Key: "variant_ref", Value: variant},
		},
		OriginalUnitPrice:   unit,
		DiscountedUnitPrice: unit,
	}
}

func shelfLine(units int) balanceEditLine {
	return balanceEditLine{
		OrderLineItemID:  "line-1",
		ShopifyVariantID: shelfVariant,
		Title:            "After the Rain Shelf",
		Units:            units,
		TargetUnitPrice:  350,
		AppBalance:       175 * float64(units),
	}
}

func errCode(err error) string {
	var appErr *apierror.AppError
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return ""
}

// The PRD example: 3 units, deposit $175 each, website price $350, remaining shipping $86.56.
func TestPlanBalanceEdit_ReplacesTheDepositAtFullPrice(t *testing.T) {
	calc := []shopify.CalculatedLineItem{depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 3, 175)}

	plan, err := planBalanceEdit([]balanceEditLine{shelfLine(3)}, calc, 86.56, "UPS Ground - Remaining 50%")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Reductions) != 1 || plan.Reductions[0].NewQuantity != 0 {
		t.Fatalf("expected the deposit line reduced to 0, got %+v", plan.Reductions)
	}
	if len(plan.Additions) != 1 || plan.Additions[0].Units != 3 || plan.Additions[0].TargetUnitPrice != 350 {
		t.Fatalf("expected 3 units of the real product at $350, got %+v", plan.Additions)
	}
	if plan.Shipping == nil || plan.Shipping.Amount != 86.56 {
		t.Fatalf("expected remaining shipping $86.56, got %+v", plan.Shipping)
	}
	// (350 - 175) x 3 + 86.56
	if plan.OutstandingIncrease != 611.56 {
		t.Fatalf("expected outstanding increase 611.56, got %v", plan.OutstandingIncrease)
	}
}

// Two of three units in this batch: the deposit line keeps one unit for the next batch.
func TestPlanBalanceEdit_MultiBatchReducesTheDepositPartially(t *testing.T) {
	calc := []shopify.CalculatedLineItem{depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 3, 175)}

	plan, err := planBalanceEdit([]balanceEditLine{shelfLine(2)}, calc, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Reductions[0].NewQuantity != 1 {
		t.Fatalf("expected 1 deposit unit left for the next batch, got %d", plan.Reductions[0].NewQuantity)
	}
	if plan.Shipping != nil {
		t.Fatalf("expected no shipping line when nothing is left to pay, got %+v", plan.Shipping)
	}
	if plan.OutstandingIncrease != 350 {
		t.Fatalf("expected 350, got %v", plan.OutstandingIncrease)
	}
}

// With a stored line ID the exact deposit line is used even when two share a variant.
func TestPlanBalanceEdit_StoredLineIDPicksTheDepositLine(t *testing.T) {
	calc := []shopify.CalculatedLineItem{
		depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 1, 175),
		depositCalcLine("gid://shopify/CalculatedLineItem/12", shelfVariant, 3, 175),
	}
	line := shelfLine(3)
	line.DepositLineID = "gid://shopify/LineItem/12"

	plan, err := planBalanceEdit([]balanceEditLine{line}, calc, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plan.Reductions[0].CalculatedLineID != "gid://shopify/CalculatedLineItem/12" {
		t.Fatalf("expected line 12, got %s", plan.Reductions[0].CalculatedLineID)
	}
}

func TestPlanBalanceEdit_RefusesWhatItCannotDoSafely(t *testing.T) {
	cases := []struct {
		name  string
		calc  []shopify.CalculatedLineItem
		lines []balanceEditLine
		code  string
	}{
		{
			name: "two deposit lines for the variant and no stored ID",
			calc: []shopify.CalculatedLineItem{
				depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 1, 175),
				depositCalcLine("gid://shopify/CalculatedLineItem/12", shelfVariant, 3, 175),
			},
			lines: []balanceEditLine{shelfLine(1)},
			code:  "deposit_line_ambiguous",
		},
		{
			name:  "no deposit line",
			calc:  []shopify.CalculatedLineItem{depositCalcLine("gid://shopify/CalculatedLineItem/11", toteVariant, 3, 175)},
			lines: []balanceEditLine{shelfLine(1)},
			code:  "deposit_line_not_found",
		},
		{
			name:  "deposit line has fewer units than the group",
			calc:  []shopify.CalculatedLineItem{depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 1, 175)},
			lines: []balanceEditLine{shelfLine(3)},
			code:  "deposit_line_short",
		},
		{
			name: "Shopify deposit price disagrees with the app balance",
			calc: []shopify.CalculatedLineItem{depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 3, 150)},
			// app expects 3 x 175 but Shopify charged 150, so the edit would bill 600
			lines: []balanceEditLine{shelfLine(3)},
			code:  "balance_mismatch",
		},
		{
			name: "no checkout price",
			calc: []shopify.CalculatedLineItem{depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 3, 175)},
			lines: []balanceEditLine{{
				OrderLineItemID: "line-1", ShopifyVariantID: shelfVariant, Title: "Shelf", Units: 3, AppBalance: 525,
			}},
			code: "balance_price_missing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planBalanceEdit(tc.lines, tc.calc, 10, "Shipping")
			if got := errCode(err); got != tc.code {
				t.Fatalf("expected %s, got %q (%v)", tc.code, got, err)
			}
		})
	}
}

// Deposit lines are matched by the variant they reference, ignoring the GID type prefix.
func TestPlanBalanceEdit_TwoItemsTwoDepositLines(t *testing.T) {
	calc := []shopify.CalculatedLineItem{
		depositCalcLine("gid://shopify/CalculatedLineItem/11", shelfVariant, 3, 175),
		depositCalcLine("gid://shopify/CalculatedLineItem/12", toteVariant, 5, 144.50),
	}
	tote := balanceEditLine{
		OrderLineItemID: "line-2", ShopifyVariantID: toteVariant, Title: "Amy Dollhouse Tote",
		Units: 5, TargetUnitPrice: 289, AppBalance: 722.50,
	}

	plan, err := planBalanceEdit([]balanceEditLine{shelfLine(3), tote}, calc, 32.92, "Shipping")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plan.Reductions) != 2 || len(plan.Additions) != 2 {
		t.Fatalf("expected two swaps, got %+v / %+v", plan.Reductions, plan.Additions)
	}
	// 525 + 722.50 + 32.92
	if plan.OutstandingIncrease != 1280.42 {
		t.Fatalf("expected 1280.42, got %v", plan.OutstandingIncrease)
	}
}

func TestMatchBalanceLines_PrefersNewLinesWithTheExactUnits(t *testing.T) {
	after := []shopify.OrderLineState{
		{ID: "gid://shopify/LineItem/11", Quantity: 3, CurrentQuantity: 0},                          // removed deposit
		{ID: "gid://shopify/LineItem/15", VariantID: shelfVariant, Quantity: 2, CurrentQuantity: 2}, // ship-ready, known
		{ID: "gid://shopify/LineItem/21", VariantID: shelfVariant, Quantity: 1, CurrentQuantity: 1}, // other new line
		{ID: "gid://shopify/LineItem/22", VariantID: shelfVariant, Quantity: 3, CurrentQuantity: 3}, // ours
	}
	known := map[string]bool{"11": true, "15": true}
	additions := []variantAddition{{OrderLineItemID: "line-1", ShopifyVariantID: shelfVariant, Title: "Shelf", Units: 3, TargetUnitPrice: 350}}

	lines, err := matchBalanceLines(after, known, additions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 1 || lines[0].ShopifyLineItemID != "gid://shopify/LineItem/22" || lines[0].OrderLineItemID != "line-1" {
		t.Fatalf("expected line 22 for line-1, got %+v", lines)
	}
}

func TestMatchBalanceLines_ReportsAMissingLine(t *testing.T) {
	additions := []variantAddition{{OrderLineItemID: "line-1", ShopifyVariantID: shelfVariant, Title: "Shelf", Units: 3}}
	if _, err := matchBalanceLines(nil, map[string]bool{}, additions); err == nil {
		t.Fatal("expected an error when the new line cannot be found")
	}
}

func TestFindShippingLine_ByStoredIDThenUniqueTitle(t *testing.T) {
	lines := []shopify.CalculatedShippingLine{
		{ID: "gid://shopify/CalculatedShippingLine/31", Title: "UPS Ground - Remaining 50% (Batch Oct)", Price: 40},
		{ID: "gid://shopify/CalculatedShippingLine/32", Title: "UPS Ground - Remaining 50% (Batch Dec)", Price: 50},
	}
	if got := findShippingLine(lines, "gid://shopify/ShippingLine/32", "anything"); got == nil || got.Price != 50 {
		t.Fatalf("expected the stored line, got %+v", got)
	}
	if got := findShippingLine(lines, "", "UPS Ground - Remaining 50% (Batch Oct)"); got == nil || got.Price != 40 {
		t.Fatalf("expected the title match, got %+v", got)
	}
	removed := []shopify.CalculatedShippingLine{{ID: "gid://shopify/CalculatedShippingLine/31", Title: "X", StagedStatus: "REMOVED"}}
	if got := findShippingLine(removed, "gid://shopify/ShippingLine/31", "X"); got != nil {
		t.Fatalf("a removed line must not be returned, got %+v", got)
	}
}

func TestNewShippingLineID(t *testing.T) {
	before := []shopify.OrderShippingLineState{{ID: "gid://shopify/ShippingLine/1", Title: "Old"}}
	after := []shopify.OrderShippingLineState{
		{ID: "gid://shopify/ShippingLine/1", Title: "Old"},
		{ID: "gid://shopify/ShippingLine/2", Title: "UPS Ground - Remaining 50%"},
	}
	if got := newShippingLineID(before, after, "UPS Ground - Remaining 50%"); got != "gid://shopify/ShippingLine/2" {
		t.Fatalf("expected line 2, got %q", got)
	}
}
