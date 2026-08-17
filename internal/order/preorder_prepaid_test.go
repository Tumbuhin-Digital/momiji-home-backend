package order

import (
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }

func strPtr(v string) *string { return &v }

// The checkout row is created by the webhook: it holds the pool, has no batch and
// usually never gets a final price of its own once every item is allocated to a batch.
func checkoutRow(id string, pool float64) PreorderShipment {
	return PreorderShipment{ID: id, PrepaidShipping: pool}
}

func TestAllocatePrepaidShipping_UnbatchedGroupConsumesWholePool(t *testing.T) {
	shipments := []PreorderShipment{
		{ID: "ship-1", PrepaidShipping: 73.81, FinalShippingPrice: f64(175)},
	}

	got := AllocatePrepaidShipping(shipments)

	if got["ship-1"] != 73.81 {
		t.Fatalf("expected full pool 73.81 applied, got %v", got["ship-1"])
	}
}

// The regression this fix exists for: the pool sits on the batch-less checkout row while
// the group that actually gets invoiced is a batch row carrying PrepaidShipping = 0.
func TestAllocatePrepaidShipping_BatchGroupInheritsPoolFromCheckoutRow(t *testing.T) {
	shipments := []PreorderShipment{
		checkoutRow("checkout", 73.81),
		{ID: "batch-a", BatchID: strPtr("batch-a-id"), FinalShippingPrice: f64(175)},
	}

	got := AllocatePrepaidShipping(shipments)

	if got["batch-a"] != 73.81 {
		t.Fatalf("expected batch group to receive the pool, got %v", got["batch-a"])
	}
}

// Two batch groups must share one prepayment: copying it to both would under-bill.
func TestAllocatePrepaidShipping_PoolSharedAcrossBatchGroupsNeverExceedsPool(t *testing.T) {
	pool := 73.81
	shipments := []PreorderShipment{
		checkoutRow("checkout", pool),
		{ID: "batch-a", BatchID: strPtr("a"), FinalShippingPrice: f64(50)},
		{ID: "batch-b", BatchID: strPtr("b"), FinalShippingPrice: f64(120)},
	}

	got := AllocatePrepaidShipping(shipments)

	// First group is capped by its own shipping; the rest flows to the next.
	if got["batch-a"] != 50 {
		t.Fatalf("expected batch-a capped at its 50.00 shipping, got %v", got["batch-a"])
	}
	if got["batch-b"] != 23.81 {
		t.Fatalf("expected batch-b to get the remaining 23.81, got %v", got["batch-b"])
	}

	// Only groups with a final price can bill, so those are the ones that consume.
	billed := got["batch-a"] + got["batch-b"]
	if billed != pool {
		t.Fatalf("total applied %v must equal the pool %v — never over- or under-deduct", billed, pool)
	}
}

// Re-reading after one group was invoiced must not hand its share out again.
func TestAllocatePrepaidShipping_InvoicedGroupKeepsShareAndDoesNotReleaseIt(t *testing.T) {
	sentAt := time.Now()
	shipments := []PreorderShipment{
		checkoutRow("checkout", 73.81),
		{
			ID:                 "batch-a",
			BatchID:            strPtr("a"),
			FinalShippingPrice: f64(50),
			PrepaidApplied:     50,
			InvoiceSentAt:      &sentAt,
		},
		{ID: "batch-b", BatchID: strPtr("b"), FinalShippingPrice: f64(120)},
	}

	got := AllocatePrepaidShipping(shipments)

	if got["batch-a"] != 50 {
		t.Fatalf("invoiced group must keep exactly what it consumed, got %v", got["batch-a"])
	}
	if got["batch-b"] != 23.81 {
		t.Fatalf("expected only the unconsumed 23.81 to remain, got %v", got["batch-b"])
	}
}

// A group invoiced before the split scheme consumed nothing and must stay at zero.
func TestAllocatePrepaidShipping_LegacyInvoicedGroupStaysZero(t *testing.T) {
	sentAt := time.Now()
	shipments := []PreorderShipment{
		{ID: "legacy", FinalShippingPrice: f64(175), InvoiceSentAt: &sentAt},
	}

	got := AllocatePrepaidShipping(shipments)

	if got["legacy"] != 0 {
		t.Fatalf("legacy invoiced group must not gain a deduction, got %v", got["legacy"])
	}
}

// Orders placed before the scheme have no pool at all and must bill shipping in full.
func TestAllocatePrepaidShipping_NoPoolLeavesEveryGroupAtZero(t *testing.T) {
	shipments := []PreorderShipment{
		{ID: "ship-1", FinalShippingPrice: f64(175)},
		{ID: "ship-2", BatchID: strPtr("b"), FinalShippingPrice: f64(90)},
	}

	got := AllocatePrepaidShipping(shipments)

	for id, v := range got {
		if v != 0 {
			t.Fatalf("shipment %s should get no deduction without a pool, got %v", id, v)
		}
	}
}

// A group whose shipping is not priced yet reports the pool so the admin is told about
// the prepayment when they open Calculate Shipping, but must not consume it — otherwise
// the group that actually bills would lose its deduction.
func TestAllocatePrepaidShipping_UnconfiguredGroupReportsPoolWithoutConsumingIt(t *testing.T) {
	shipments := []PreorderShipment{
		checkoutRow("checkout", 73.81),
		{ID: "batch-a", BatchID: strPtr("a")}, // shipping not calculated yet
		{ID: "batch-b", BatchID: strPtr("b"), FinalShippingPrice: f64(120)},
	}

	got := AllocatePrepaidShipping(shipments)

	if got["batch-a"] != 73.81 {
		t.Fatalf("unpriced group should surface the available pool, got %v", got["batch-a"])
	}
	if got["batch-b"] != 73.81 {
		t.Fatalf("expected the pool to still be available for batch-b, got %v", got["batch-b"])
	}
}

// Shipping cheaper than what was prepaid must not produce a negative invoice line.
func TestAllocatePrepaidShipping_DeductionNeverExceedsGroupShipping(t *testing.T) {
	shipments := []PreorderShipment{
		{ID: "ship-1", PrepaidShipping: 73.81, FinalShippingPrice: f64(20)},
	}

	got := AllocatePrepaidShipping(shipments)

	if got["ship-1"] != 20 {
		t.Fatalf("expected deduction capped at the 20.00 shipping, got %v", got["ship-1"])
	}
}

func TestPrepaidShippingPool_SumsAcrossRows(t *testing.T) {
	shipments := []PreorderShipment{
		{ID: "a", PrepaidShipping: 73.81},
		{ID: "b"},
	}

	if got := PrepaidShippingPool(shipments); got != 73.81 {
		t.Fatalf("expected pool 73.81, got %v", got)
	}
}

// A group allocated nothing must say so explicitly. Emitting no value let clients fall
// back to the order-level row and credit this invoice with another group's prepayment.
func TestShipmentDTOReportsZeroAllocationExplicitly(t *testing.T) {
	svc := &service{}
	batch := PreorderShipment{ID: "batch-a", BatchID: strPtr("a"), FinalShippingPrice: f64(43.43)}
	allocation := map[string]float64{"batch-a": 0}

	dto := svc.toPreorderShipmentDTOWithAllocation(&batch, allocation)

	if dto.PrepaidShipping == nil {
		t.Fatal("expected an explicit prepaid figure, got nil (clients would fall back)")
	}
	if *dto.PrepaidShipping != "0.00" {
		t.Fatalf("expected \"0.00\", got %q", *dto.PrepaidShipping)
	}
}

// End-to-end of the reported case: pool consumed by the unbatched group, so the batch
// group bills its shipping in full.
func TestAllocatePrepaidShipping_ORD1304Shape(t *testing.T) {
	shipments := []PreorderShipment{
		{ID: "unbatched", PrepaidShipping: 138.95, FinalShippingPrice: f64(234.48)},
		{ID: "batch", BatchID: strPtr("sept-2028"), FinalShippingPrice: f64(43.43)},
	}

	got := AllocatePrepaidShipping(shipments)

	if got["unbatched"] != 138.95 {
		t.Fatalf("unbatched group should absorb the pool, got %v", got["unbatched"])
	}
	if got["batch"] != 0 {
		t.Fatalf("batch group must get nothing once the pool is spent, got %v", got["batch"])
	}

	billed := (234.48 - got["unbatched"]) + (43.43 - got["batch"])
	if round2(billed) != 138.96 {
		t.Fatalf("expected 138.96 still to bill, got %v", round2(billed))
	}
	if round2(138.95+billed) != round2(234.48+43.43) {
		t.Fatal("prepaid plus billed must equal the sum of final shipping prices")
	}
}
