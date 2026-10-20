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

// PrepaidShippingLimits is the most of the checkout prepayment each group could deduct if
// its final shipping price were high enough — AllocatePrepaidShipping's figure before the
// cap at the group's own price. The shipping editor previews the invoice with it: a price
// change moves the deduction anywhere between 0 and this limit, so previewing with the
// capped allocation would show the wrong amount whenever the price goes up.
//
// A paid group is fixed at what it consumed. A group invoiced but not yet paid can still be
// re-priced, so it may reclaim its own share plus whatever no other group has consumed.
func PrepaidShippingLimits(shipments []PreorderShipment) map[string]float64 {
	out := make(map[string]float64, len(shipments))

	pool := PrepaidShippingPool(shipments)
	consumed := prepaidShippingConsumed(shipments)
	available := round2(pool - consumed)
	if available < 0 {
		available = 0
	}

	for i := range shipments {
		sh := &shipments[i]

		if sh.InvoicePaidAt != nil {
			out[sh.ID] = round2(sh.PrepaidApplied)
			continue
		}

		if sh.PrepaidApplied > 0 || sh.InvoiceSentAt != nil {
			limit := round2(pool - (consumed - sh.PrepaidApplied))
			if limit < 0 {
				limit = 0
			}
			out[sh.ID] = limit
			continue
		}

		// Mirrors AllocatePrepaidShipping so later groups see the same remaining pool.
		out[sh.ID] = available
		if sh.FinalShippingPrice != nil {
			deduct := available
			if final := round2(*sh.FinalShippingPrice); final < deduct {
				deduct = final
			}
			if deduct > 0 {
				available = round2(available - deduct)
			}
		}
	}

	return out
}

// RepricedPrepaidApplied is how much of the prepayment an invoiced but unpaid group
// consumes once its final shipping price changes to finalPrice. The group gives up its old
// share and takes again from what the other groups left, so the total handed out still
// never exceeds what the customer paid.
func RepricedPrepaidApplied(shipments []PreorderShipment, shipmentID string, finalPrice float64) float64 {
	applied := PrepaidShippingLimits(shipments)[shipmentID]
	if final := round2(finalPrice); final < applied {
		applied = final
	}
	if applied < 0 {
		return 0
	}
	return applied
}
