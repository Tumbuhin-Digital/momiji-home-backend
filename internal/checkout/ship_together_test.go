package checkout

import (
	"context"
	"testing"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/cart"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/preorderbatch"
)

type stubBatchPreviewer struct {
	result *preorderbatch.AllocateResult
	err    error
}

func (s *stubBatchPreviewer) PreviewAllocation(context.Context, string, int, *string, *string) (*preorderbatch.AllocateResult, error) {
	return s.result, s.err
}

func TestValidateShipTogether_RequiresMixedCart(t *testing.T) {
	t.Parallel()

	mixed := &cart.CartResponse{
		ShipReady: []cart.CartItem{{VariantID: "v1"}},
		PreOrder:  []cart.CartItem{{VariantID: "v2"}},
	}
	if err := validateShipTogether(mixed, true); err != nil {
		t.Fatalf("expected mixed cart to pass, got %v", err)
	}

	shipOnly := &cart.CartResponse{ShipReady: []cart.CartItem{{VariantID: "v1"}}}
	if err := validateShipTogether(shipOnly, true); err == nil {
		t.Fatal("expected ship-only cart to fail ship_together validation")
	}
}

func TestApplyShipTogetherSegments_ReclassifiesShipReady(t *testing.T) {
	t.Parallel()

	shipReady := []cart.CartItem{
		{VariantID: "v1", Quantity: 2, UnitPrice: "100.00", Title: "Ready"},
	}
	preOrder := []cart.CartItem{
		{VariantID: "v2", Quantity: 1, UnitPrice: "50.00", Title: "Pre"},
	}

	gotShip, gotPre := applyShipTogetherSegments(shipReady, preOrder, false)
	if len(gotShip) != 1 || len(gotPre) != 1 {
		t.Fatalf("unticked: expected segments unchanged, got ship=%d pre=%d", len(gotShip), len(gotPre))
	}

	gotShip, gotPre = applyShipTogetherSegments(shipReady, preOrder, true)
	if len(gotShip) != 0 {
		t.Fatalf("ticked: expected no ship-ready items, got %d", len(gotShip))
	}
	if len(gotPre) != 2 {
		t.Fatalf("ticked: expected 2 preorder items, got %d", len(gotPre))
	}

	lines := buildDraftLinesFromSegments(gotShip, gotPre)
	if len(lines) != 2 {
		t.Fatalf("expected 2 draft lines, got %d", len(lines))
	}
	for i, line := range lines {
		if line.VariantID != "" {
			t.Fatalf("line %d should be custom preorder (no variant id), got %q", i, line.VariantID)
		}
		hasPreorderType := false
		for _, attr := range line.CustomAttributes {
			if attr.Key == "type" && attr.Value == "preorder_dp" {
				hasPreorderType = true
			}
		}
		if !hasPreorderType {
			t.Fatalf("line %d missing preorder_dp attribute: %+v", i, line)
		}
	}
}

func TestApplyShipTogetherSegments_MergesSameVariant(t *testing.T) {
	t.Parallel()

	shipReady := []cart.CartItem{
		{VariantID: "v1", Quantity: 1, UnitPrice: "40.00", Title: "Same"},
	}
	preOrder := []cart.CartItem{
		{VariantID: "v1", Quantity: 3, UnitPrice: "40.00", Title: "Same"},
	}

	_, gotPre := applyShipTogetherSegments(shipReady, preOrder, true)
	if len(gotPre) != 1 {
		t.Fatalf("expected merged single line, got %d", len(gotPre))
	}
	if gotPre[0].Quantity != 4 {
		t.Fatalf("expected qty 4, got %d", gotPre[0].Quantity)
	}
	if gotPre[0].Subtotal != "160.00" {
		t.Fatalf("expected subtotal 160.00, got %q", gotPre[0].Subtotal)
	}
}

func TestFormatShipTogetherHoldNote(t *testing.T) {
	t.Parallel()

	got := formatShipTogetherHoldNote("September Batch")
	want := "DO NOT FULFIL ANY ITEM IN THIS ORDER UNTIL September Batch"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCollectCheckoutBatchNames(t *testing.T) {
	t.Parallel()

	previewer := &stubBatchPreviewer{
		result: &preorderbatch.AllocateResult{
			Allocations: []preorderbatch.BatchAllocationLine{
				{BatchName: "October Batch"},
				{BatchName: "September Batch"},
			},
		},
	}

	names, err := collectCheckoutBatchNames(
		context.Background(),
		previewer,
		[]cart.CartItem{{VariantID: "v1", Quantity: 2}},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if names != "October Batch, September Batch" {
		t.Fatalf("got %q", names)
	}
}
