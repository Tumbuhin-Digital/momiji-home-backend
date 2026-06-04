# Phase 10: Final Implementation — Remaining PRD Features

> **Status:** 🔲 PENDING
> **Date:** 2026-06-04
> **Pre-requisite:** Phase 8 (Email), Phase 9 (Checkout + Webhook), API Contract Compliance, Cart Fix

---

## What Is Already Implemented (Full Inventory)

| Module | Status | Notes |
|--------|--------|-------|
| Auth (login, logout, refresh, RBAC) | ✅ Done | |
| Guest session | ✅ Done | |
| Cart (add, update, remove, clear, merge) | ✅ Done | `fulfillment_type` fix pending (separate plan) |
| Products (list, detail, variants, sync, status, price, batch) | ✅ Done | DTO field gaps pending (API Contract Compliance plan) |
| Orders (create, list, detail, accept, cancel, step, received) | ✅ Done | |
| Pre-order settlements (list, detail, invoice, mark paid) | ✅ Done | |
| D+3 / D+6 reminders scheduler | ✅ Done | |
| D+7 expired status + email | ✅ Done | `ProcessReminders` marks status + fires `SendExpired` |
| Email: order confirmation, invoice, reminder, expired, settlement paid | ✅ Done | |
| `SendShipmentDispatched` email method | ✅ Defined | Not yet triggered — no tracking endpoint |
| Webhook: `orders/create` / `orders/paid` | ✅ Done | |
| Webhook: `inventory_levels/update` | ✅ Done | |
| Nightly product sync scheduler | ✅ Done | |
| `GET /checkout/confirm` | ⚠️ Stub | Returns hardcoded mock, not real DB lookup |
| Shopify Refund API call (D+7 80% refund) | ❌ Missing | Status set to expired, but no Shopify refund API called |
| Tracking number handling | ❌ Missing | No endpoint, no DB field, email not triggered |
| Stock lock at checkout (15-min reserve) | ❌ Missing | PRD requirement, not in any plan |
| Fulfillment creation (`POST /fulfillments.json`) | ❌ Missing | PRD requires backend to trigger Shopify fulfillment |
| Cart `fulfillment_type` backend derivation | ❌ Missing | Separate plan: `2026-06-04-cart-ux-and-fulfillment-type-fix.md` |
| API contract field compliance | ❌ Missing | Separate plan: `2026-06-01-api-contract-compliance.md` |

---

## Phase 10 Scope

Four features remain from the PRD that have no implementation:

1. **80% Refund on D+7 Expiry** — Shopify API call is missing. Status flip exists, money not returned.
2. **Tracking Number & Shipment Dispatch** — No endpoint, no DB column, email method exists but never called.
3. **`GET /checkout/confirm`** — Mock response, needs real DB lookup.
4. **Stock Lock at Checkout** — PRD mandates 15-minute reservation before payment.

> **Note:** Stock lock is the most complex piece. Given it requires a TTL mechanism and does not block MVP payment (Shopify handles final payment conflict), it is marked as **lower priority** and can be deferred post-launch if timeline is tight. The others are required for the full business flow.

---

## Part 1 — 80% Refund on D+7 Expiry

### Problem

`ProcessReminders` in `internal/preorder/service.go:244-258` correctly marks settlements as `expired` and fires `SendExpired` email. But it never calls the Shopify Refund API — the customer's 50% deposit is NOT actually returned.

PRD rule: *"Expired (D+7): refund 80% (20% admin fee)"*

### Required Changes

#### [MODIFY] `internal/platform/shopify/client.go`
- Add `CreateRefund(ctx, shopifyOrderID string, amount float64, currency string, reason string) error`
- Calls Shopify Admin REST: `POST /admin/api/2024-04/orders/{id}/refunds.json`
- Payload: `{ "refund": { "currency": "USD", "note": "Pre-order expired — auto-refund 80%", "transactions": [{ "kind": "refund", "gateway": "shopify_payments", "amount": "<80% of dp>" }] } }`

#### [MODIFY] `internal/platform/shopify/client.go` (interface)
- Add `CreateRefund` to the `Client` interface so it can be mocked in tests

#### [MODIFY] `internal/preorder/service.go` — `ProcessReminders` D+7 block
- After marking status `expired`, check if the settlement has an associated `ShopifyOrderID` on the parent order
- If yes: calculate `refundAmount = balanceAmount * 0.8` (80% of the deposit, not the full order)
  - Note: `balance_amount` on the settlement is the 50% balance yet to be paid. The actual deposit paid = `balance_amount` (since deposit = balance in our 50/50 model). So `refundAmount = settlement.BalanceAmount * 0.8`.
- Call `shopClient.CreateRefund(ctx, shopifyOrderID, refundAmount, "USD", "Pre-order expired")`
- Log success or failure (non-fatal — do not block the expiry status update)

#### [MODIFY] `internal/preorder/service.go` — inject `shopClient`
- Currently `preorder.service` does not have `shopify.Client`. Add it to the struct and constructor.

#### [MODIFY] `cmd/server/main.go`
- Pass `shopifyClient` to `preorder.NewPreorderService()`

---

## Part 2 — Tracking Number & Shipment Dispatch Email

### Problem

The `SendShipmentDispatched` email method exists in `internal/platform/email/service.go` but is never called. There is no way to associate a tracking number with an order or trigger the shipment email to the customer.

PRD flow: Admin adds tracking → backend triggers email with tracking link.

### Required Changes

#### [NEW] Migration — add `tracking_number` and `tracking_url` to order line items

```sql
-- 015_add_tracking_fields.up.sql
ALTER TABLE order_line_items
  ADD COLUMN IF NOT EXISTS tracking_number VARCHAR(255),
  ADD COLUMN IF NOT EXISTS tracking_url    VARCHAR(512),
  ADD COLUMN IF NOT EXISTS shipped_at      TIMESTAMPTZ;
```

#### [MODIFY] `internal/order/model.go`
- Add `TrackingNumber *string`, `TrackingURL *string`, `ShippedAt *time.Time` to `OrderItem`

#### [NEW] API Endpoint: `PATCH /api/v1/orders/:orderId/items/:itemId/tracking`

**Request body:**
```json
{ "tracking_number": "1Z999AA10123456784", "tracking_url": "https://www.ups.com/track?tracknum=..." }
```

**Handler:** Admin-only (requires Bearer + RBAC admin)

**Service behavior:**
1. Update `order_line_items.tracking_number`, `tracking_url`, `shipped_at = now()`
2. Update `item_status = "shipped"`, `fulfillment_step = 3` for that item
3. Fire `emailService.SendShipmentDispatched()` in a goroutine to the customer

#### [MODIFY] `internal/order/service.go`
- Add `AddTrackingNumber(ctx, userID, orderID, itemID, trackingNumber, trackingURL string) error` to `OrderService` interface and implementation

#### [MODIFY] `internal/order/handler.go`
- Add handler `AddTrackingNumber` and register route `PATCH /orders/:orderId/items/:itemId/tracking` under admin middleware

#### [MODIFY] `internal/platform/email/service.go` — `ShipmentEmailData`
- Verify `ShipmentEmailData` struct has `TrackingNumber` and `TrackingURL` fields. Add if missing.

---

## Part 3 — `GET /checkout/confirm` Real Implementation

### Problem

`internal/checkout/handler.go:190-204` returns a hardcoded mock response. A real Shopify order may already exist in our DB after the `orders/create` webhook fires.

### Required Changes

#### [MODIFY] `internal/checkout/handler.go`
- `GetCheckoutConfirm` currently does not access `OrderStore`. The handler needs access to `OrderService` or a dedicated order lookup.

#### [MODIFY] `internal/checkout/handler.go` — `Handler` struct
- Add `orderService order.OrderService` (or a minimal interface with just `GetOrderByShopifyID`)

#### [MODIFY] `internal/order/service.go`
- Add `GetOrderByShopifyID(ctx, shopifyOrderID string) (*OrderResponse, error)` to `OrderService` interface

#### [MODIFY] `internal/order/store.go` + `internal/order/postgres.go`
- Add `GetOrderByShopifyID(ctx, shopifyOrderID string) (*Order, error)` to `Store` interface and Postgres implementation
- Query: `WHERE shopify_order_id = ?`

#### [MODIFY] `internal/checkout/handler.go` — `GetCheckoutConfirm`
- Replace mock with: look up order by `shopify_order_id` query param
- If found: return `{ order_id, order_number, status, shopify_order_id }`
- If not found: return `404` — webhook hasn't fired yet, frontend should retry

#### [MODIFY] `cmd/server/main.go`
- Pass `orderService` to `checkout.NewCheckoutHandler()`

---

## Part 4 — Stock Lock at Checkout (15-Minute Reserve)

> **Priority: Lower.** Shopify Payments itself is last-call authoritative — if two users check out the last item, Shopify will reject the second payment. This is a UX improvement (warn the user before they even reach Shopify), not a payment safety mechanism. Can be deferred post-launch.

### Design

PRD rule: *"Stock lock 15 menit saat checkout dimulai"*

When `POST /api/v1/checkout` is called:
1. For each `ship_ready` item in the cart, check `inventory_quantity` in our DB
2. If `available ≥ quantity_in_cart`: mark a lock in a `stock_locks` table with TTL = 15 min
3. If `available < quantity_in_cart`: return `422 out_of_stock` before creating the Shopify Cart
4. A background cleanup job (or check on each lock query) removes expired locks

### Required Changes

#### [NEW] Migration — `stock_locks` table

```sql
-- 016_add_stock_locks.up.sql
CREATE TABLE IF NOT EXISTS stock_locks (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shopify_variant_id VARCHAR(255) NOT NULL,
  quantity        INT NOT NULL,
  session_id      VARCHAR(255),
  user_id         UUID,
  expires_at      TIMESTAMPTZ NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_stock_locks_variant ON stock_locks(shopify_variant_id, expires_at);
```

#### [NEW] `internal/cart/stocklock.go` (or `internal/checkout/stocklock.go`)
- `AcquireLocks(ctx, cartID, variantItems []LockRequest) error` — checks available qty minus active locks, creates lock rows
- `ReleaseLocks(ctx, userID or sessionID) error` — deletes locks for this session (called after webhook confirms payment)
- `CleanExpiredLocks(ctx) error` — called by scheduler to purge stale locks

#### [MODIFY] `internal/checkout/service.go` — `InitiateCheckout`
- Call `AcquireLocks` before `CreateStorefrontCart`
- Return `422` if any item is locked out

#### [MODIFY] `internal/platform/scheduler/scheduler.go`
- Add cleanup job every 5 minutes: `stockLockService.CleanExpiredLocks(ctx)`

---

## Recommended Implementation Order

```
Week 1
├── Part 3 — GET /checkout/confirm (small, unblocks FE order-confirmed page)
└── Part 1 — 80% refund on D+7 (required for business correctness)

Week 2
├── Part 2 — Tracking number endpoint + shipment email
└── Cart fulfillment_type fix (from 2026-06-04 plan)

Post-launch (if timeline tight)
└── Part 4 — Stock lock at checkout
```

---

## Verification Plan

```bash
# Part 1 — D+7 refund
# Set a settlement's invoiced_at to 7 days ago in DB, run ProcessReminders
# Check Shopify admin for refund record
# Check customer email for expiry notification

# Part 2 — Tracking
curl -X PATCH http://localhost:3000/api/v1/orders/:orderId/items/:itemId/tracking \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"tracking_number": "1Z999AA10123456784", "tracking_url": "https://ups.com/track?n=..."}'
# Check Mailpit for shipment dispatched email

# Part 3 — checkout confirm
# After webhook fires for a test order:
curl "http://localhost:3000/checkout/confirm?shopify_order_id=12345"
# Expected: real order data, not mock

# Part 4 — stock lock
# Add item with inventory=1 to two separate sessions
# Both POST /checkout simultaneously
# Second one should receive 422 out_of_stock
```
