package order

import "math"

// Shipping for a pre-order is split 50/50 across the two payments, but the upfront half
// is collected ONCE for the whole order while settlement happens per fulfillment group.
// Copying that half onto every group would under-bill (each group deducts it again);
// ignoring it on batch groups over-bills. So it is treated as an order-level pool that
// groups consume, and each group records what it actually used in PrepaidApplied.

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// PrepaidShippingPool is the total shipping prepayment collected at checkout for an
// order. Only the checkout-created shipment row carries a value, so the sum is that
// amount; summing rather than picking one row keeps it correct if that ever changes.
func PrepaidShippingPool(shipments []PreorderShipment) float64 {
	total := 0.0
	for i := range shipments {
		total += shipments[i].PrepaidShipping
	}
	return round2(total)
}

// prepaidShippingConsumed is how much of the pool earlier settlement invoices already used.
func prepaidShippingConsumed(shipments []PreorderShipment) float64 {
	total := 0.0
	for i := range shipments {
		total += shipments[i].PrepaidApplied
	}
	return round2(total)
}

// AllocatePrepaidShipping decides how much of the checkout prepayment each shipment may
// deduct from its settlement invoice, keyed by shipment ID.
//
// Groups that already sent an invoice keep exactly what they consumed, so re-running this
// never moves money that was already billed. Whatever is left is offered to the remaining
// groups in creation order, capped by each group's own final shipping price so a group can
// never deduct more than it bills. The total handed out therefore never exceeds what the
// customer actually paid, whichever order the admin invoices the groups in.
//
// Shipments pass through unchanged when the pool is empty (legacy orders placed before the
// split-shipping scheme), which bills their shipping in full — the old behaviour.
func AllocatePrepaidShipping(shipments []PreorderShipment) map[string]float64 {
	out := make(map[string]float64, len(shipments))

	available := round2(PrepaidShippingPool(shipments) - prepaidShippingConsumed(shipments))
	if available < 0 {
		available = 0
	}

	for i := range shipments {
		sh := &shipments[i]

		// Already invoiced: locked to what it consumed, even if that was zero.
		if sh.PrepaidApplied > 0 || sh.InvoiceSentAt != nil {
			out[sh.ID] = round2(sh.PrepaidApplied)
			continue
		}

		if available <= 0 {
			out[sh.ID] = 0
			continue
		}

		// No final price yet: report what is still available so the admin sees the
		// prepayment the first time they open Calculate Shipping — that is exactly when
		// they need to be told to enter the FULL shipping price. Nothing is consumed here,
		// because this group has no invoice to offset yet. Billing never takes this branch:
		// RequestSecondPayment refuses to run without a final price.
		if sh.FinalShippingPrice == nil {
			out[sh.ID] = available
			continue
		}

		deduct := available
		if final := round2(*sh.FinalShippingPrice); final < deduct {
			deduct = final
		}
		if deduct < 0 {
			deduct = 0
		}

		out[sh.ID] = deduct
		available = round2(available - deduct)
	}

	return out
}
