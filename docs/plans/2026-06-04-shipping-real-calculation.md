# Real Shipping Cost Calculation — Dimension-Based Flat Rate Tiers

> **Status:** 🔲 PENDING
> **Date:** 2026-06-04
> **Depends on:** Phase 10 (tracking fields migration) for migration numbering
> **Decision basis:** PRD + PO alignment session 2026-06-04

---

## Background & Decision

### Current State

Both `/shipping/methods` and `/shipping/calculate` return hardcoded values with no relationship to the actual cart contents, product dimensions, or destination address. This is a mock implementation.

### PRD Mandate

The PRD defines two separate shipments with different cost calculation timing:

> *"Initial payment = 100% ready + 50% preorder + **ship #1** + tax"*
> *"Pelunasan = 50% sisa preorder + **ship #2** + tax (**tax & shipping recalc saat invoice**)"*

**Ship #1** covers only `ship_ready` items and is charged at initial checkout.
**Ship #2** covers only `pre_order` items and is **recalculated fresh** at pelunasan invoice time — not locked at checkout.

### Decisions Made

| Question | Decision |
|----------|----------|
| Option 1 (both upfront) vs Option 2 (ship#2 at invoice)? | **Option 2** — mandated by PRD "recalc saat invoice" |
| Real carrier API (UPS/FedEx) or flat rate tiers? | **Flat rate tiers for MVP** — carrier API is deferred |
| Source of product dimensions? | **`product_variants` DB** — admin inputs per variant via dashboard |
| What to show for ship#2 at initial checkout? | **Estimate only** — labeled as "final amount recalculated at invoice" |

---

## How Flat Rate Tiers Work

Total shipment weight = sum of `(variant.weight_kg × quantity)` for all items in the group.

| Tier | Total Weight | Cost |
|------|-------------|------|
| XS | 0 – 2 kg | $15.00 |
| S | 2 – 5 kg | $20.00 |
| M | 5 – 15 kg | $30.00 |
| L | 15 – 30 kg | $45.00 |
| XL | > 30 kg | $60.00 |

> These are placeholder values. The actual tier table should be confirmed with the business/PO before implementation. The tier boundaries and prices should live in config or a DB table, not hardcoded.

Each shipping method (`ground`, `expedited`, `next_business_day`) applies a multiplier on top of the tier base cost:

| Method | Multiplier |
|--------|-----------|
| `ground` | 1.0× |
| `expedited` | 1.5× |
| `next_business_day` | 2.0× |

Example: 8 kg cart, ground → tier M ($30.00) × 1.0 = **$30.00**
Example: 8 kg cart, expedited → tier M ($30.00) × 1.5 = **$45.00**

---

## Proposed Changes

### Part 1 — Product Dimensions in DB

#### [NEW] Migration — add dimension fields to `product_variants`

```sql
-- 017_add_product_dimensions.up.sql
ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS weight_kg   NUMERIC(8,3) DEFAULT 0,
  ADD COLUMN IF NOT EXISTS width_cm    NUMERIC(8,1) DEFAULT 0,
  ADD COLUMN IF NOT EXISTS height_cm   NUMERIC(8,1) DEFAULT 0,
  ADD COLUMN IF NOT EXISTS depth_cm    NUMERIC(8,1) DEFAULT 0;
```

```sql
-- 017_add_product_dimensions.down.sql
ALTER TABLE product_variants
  DROP COLUMN IF EXISTS weight_kg,
  DROP COLUMN IF EXISTS width_cm,
  DROP COLUMN IF EXISTS height_cm,
  DROP COLUMN IF EXISTS depth_cm;
```

#### [MODIFY] `internal/product/model.go`
- Add `WeightKg float64`, `WidthCm float64`, `HeightCm float64`, `DepthCm float64` to `ProductVariant`

#### [MODIFY] `internal/product/dto.go` — `VariantDTO`
- Add `WeightKg float64 json:"weight_kg"` (already in contract as `weight` + `weight_unit`)

#### [MODIFY] `internal/product/service.go` — `mapVariantToDTO()`
- Map dimension fields

---

### Part 2 — Admin Dashboard: Input Dimensions per Variant

A new admin endpoint to update variant dimensions without going through a full Shopify sync.

#### [NEW] API Endpoint: `PATCH /api/v1/products/variant/:variantId/dimensions`

**Auth:** Admin only (Bearer + RBAC)

**Request body:**
```json
{
  "weight_kg": 3.5,
  "width_cm": 40.0,
  "height_cm": 20.0,
  "depth_cm": 15.0
}
```

**Response:** Updated variant dimensions echoed back.

#### [MODIFY] `internal/product/handler.go`
- Add `UpdateVariantDimensions` handler
- Register route `PATCH /products/variant/:id/dimensions` under admin middleware

#### [MODIFY] `internal/product/service.go`
- Add `UpdateVariantDimensions(ctx, variantID string, weight, width, height, depth float64) error`

#### [MODIFY] `internal/product/store.go` + `internal/product/postgres.go`
- Add `UpdateVariantDimensions(ctx, variantID string, weight, width, height, depth float64) error`

---

### Part 3 — Shipping Tier Calculation Service

#### [NEW] `internal/checkout/shipping.go`

A pure calculation module — no DB access, no external calls.

Responsibilities:
- `CalculateShipCost(weightKg float64, method string) (float64, error)`
  - Look up tier by total weight
  - Apply method multiplier
  - Return cost in USD
- `GetShippingTiers() []ShippingTier` — returns the tier table for `GET /shipping/methods`
- Expose tier table via config so it can be updated without code changes

---

### Part 4 — Real `GET /shipping/methods`

Replace the current hardcoded mock with dynamic content.

#### [MODIFY] `internal/checkout/handler.go` — `GetShippingMethods`

Currently hardcoded. Replace with:
- Read the tier table from the shipping service
- Return all three methods (`ground`, `expedited`, `next_business_day`) with their costs based on the **current cart weight**
- Cart weight = sum of `variant.weight_kg × quantity` for `ship_ready` items only

**Note:** `GET /shipping/methods` needs cart access to show accurate costs. Options:
- Pass cart weight as a query param: `GET /shipping/methods?cart_weight_kg=8.5` (simpler, FE computes weight from product data)
- Read from cart directly (requires session/user) — better but more coupling

> **Recommended:** Keep it as a query param for simplicity. Frontend knows the cart items and their weights from `GET /products`, so it can sum and pass `cart_weight_kg`.

If `cart_weight_kg` is not provided, fall back to showing base tier (XS) costs as a rough estimate.

---

### Part 5 — Real `POST /shipping/calculate`

Replace mock with real tier calculation.

#### [MODIFY] `internal/checkout/handler.go` — `CalculateShipping`

Current request body only has `shipping_method`. Expand to accept cart context.

**Updated request:**
```json
{
  "shipping_method": "ground",
  "cart_weight_kg": 8.5
}
```

Or alternatively resolve weight server-side from the user's active cart:
```json
{
  "shipping_method": "ground"
}
```
Handler reads active cart → sums `ship_ready` item weights from product store → calculates.

> **Recommended:** Server-side resolution. Don't trust client-provided weight. Cart is available from session/auth context.

**Response:**
```json
{
  "shipping_method": "ground",
  "total_weight_kg": 8.5,
  "estimated_arrival": "5-7 Business Days",
  "cost": "30.00",
  "currency": "USD"
}
```

---

### Part 6 — Ship #1 in `POST /checkout/summary` and `POST /checkout`

#### [MODIFY] `internal/checkout/handler.go` — `GetCheckoutSummary`

Currently uses hardcoded `shippingCost = 20.00`. Replace with:
1. Sum weight of all `ship_ready` items in cart (query product store for each variant's `weight_kg`)
2. Call `CalculateShipCost(totalWeight, req.ShippingMethod)` → real cost

Also add ship#2 estimate to the response:
```json
"shipping": {
  "ship1": { "method": "ground", "cost": "30.00", "items": "ship_ready only" },
  "ship2_estimate": { "cost": "~$20.00", "note": "Final amount recalculated at pelunasan invoice" }
}
```

---

### Part 7 — Ship #2 at Pelunasan Invoice Time

#### [MODIFY] `internal/preorder/service.go` — `InvoiceSettlement`

When `PATCH /preorders/settlements/:id/invoice` is called:
1. Look up the pre-order line item(s) associated with this settlement
2. Sum their `weight_kg × quantity`
3. Call `CalculateShipCost(weight, defaultMethod)` — use `ground` as default for ship#2 unless the customer has specified a preference
4. The invoice email (`SendInvoice`) should include the recalculated ship#2 cost
5. Store the calculated ship#2 cost on the settlement record

#### [NEW] Migration — add `ship2_cost` to settlements

```sql
-- (can be added to migration 017 or as a separate one)
ALTER TABLE settlements
  ADD COLUMN IF NOT EXISTS ship2_cost NUMERIC(10,2) DEFAULT 0;
```

---

### Part 8 — Shipping Method for Ship #2

Ship#2 method should default to the same method the customer chose at initial checkout. Store it on the order.

#### [NEW] Migration — add `shipping_method` to `orders` table

```sql
ALTER TABLE orders
  ADD COLUMN IF NOT EXISTS shipping_method VARCHAR(50) DEFAULT 'ground';
```

#### [MODIFY] `internal/order/service.go` — `CreateOrder` / `HandleOrderPaid` (webhook)
- Persist `req.ShippingMethod` to `orders.shipping_method`

#### [MODIFY] `internal/preorder/service.go` — `InvoiceSettlement`
- Read `orders.shipping_method` when computing ship#2 cost

---

## Migration Numbering Note

Current known migrations go up to `014`. Phase 10 (tracking) adds `015`. This plan should use:
- `017_add_product_dimensions.sql` (or check current last migration number and sequence accordingly)
- Coordinate with Phase 10 team before running migrations

---

## Verification Plan

```bash
# 1. Admin inputs dimensions for a variant
curl -X PATCH http://localhost:3000/api/v1/products/variant/:id/dimensions \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"weight_kg": 8.5, "width_cm": 40, "height_cm": 20, "depth_cm": 15}'
# Expected: 200 OK, dimensions echoed back

# 2. Get shipping methods (should show real tier costs, not hardcoded)
curl "http://localhost:3000/api/v1/shipping/methods" \
  -H "Authorization: Bearer <token>"
# Expected: 3 methods with tier-based costs, not hardcoded $20/$35

# 3. Calculate shipping with a cart that has a ship_ready item of 8.5kg
curl -X POST http://localhost:3000/api/v1/shipping/calculate \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"shipping_method": "expedited"}'
# Expected: cost = tier_M_base ($30) × 1.5 = $45.00

# 4. Checkout summary — ship1 should reflect real weight-based cost
curl -X POST http://localhost:3000/api/v1/checkout/summary \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"shipping_method": "ground", "address_id": 1}'
# Expected: ship1 cost is real, ship2 shows estimate with disclaimer

# 5. Invoice settlement — ship2 cost recalculated fresh
curl -X PATCH http://localhost:3000/api/v1/preorders/settlements/5/invoice \
  -H "Authorization: Bearer <admin_token>"
# Expected: invoice email contains recalculated ship2 cost
# Check Mailpit: shipping cost in email should match tier calc for pre-order item weight
```

---

## Summary for FE Team

| Change | Impact on FE |
|--------|-------------|
| `GET /shipping/methods` returns 3 methods (not 2) | Add `next_business_day` option to shipping selector |
| Costs are now weight-based, not hardcoded | No change needed — just consume the `cost` field as before |
| `POST /checkout/summary` response adds `ship2_estimate` | Display ship#2 estimate with disclaimer text: *"Final amount recalculated at invoice"* |
| Admin can input dimensions per variant | New UI needed in admin dashboard: dimension fields on variant edit page |
