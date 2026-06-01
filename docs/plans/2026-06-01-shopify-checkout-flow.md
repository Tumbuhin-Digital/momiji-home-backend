# Phase 9: Shopify-Native Checkout Flow

> **Status:** 🔲 PENDING
> **Date:** 2026-06-01
> **Pre-requisite:** Phase 8 (Email Notifications) in progress

---

## Problem Statement

The current `POST /api/v1/orders` creates an order **directly in our database** but does not integrate payment at checkout. Payment is not captured, and stock is not deducted in Shopify.

The frontend team requires a new flow:

1. Frontend submits shipping address + method → Backend creates a Shopify Checkout → Backend returns a `checkout_url`
2. Frontend redirects the customer to Shopify's hosted payment page
3. After payment succeeds, Shopify notifies the backend (via webhook or redirect) → Backend confirms the order, deducts stock, and saves the order to the database

---

## Key Decisions

### Why NOT reuse the existing `POST /api/v1/orders`?

The existing endpoint creates an order record **before payment** (it assumes payment happens externally). The new flow defers order creation until **payment is confirmed** by Shopify. These are different responsibilities and must remain separate.

### Which Shopify API for Checkout?

| Option | API | Notes |
|--------|-----|-------|
| ~~Storefront `checkoutCreate`~~ | Storefront GraphQL | ❌ **Deprecated for stores created after mid-2024** — our store qualifies. Cannot use. |
| Storefront Cart API | `cartCreate` → `checkoutUrl` | ✅ **Recommended replacement.** Creates a Shopify Cart, then returns a `checkoutUrl` permalink for redirect. |
| Admin Draft Order | Admin GraphQL `draftOrderCreate` | Used for pre-order items ONLY (PRD requires this for the pelunasan flow). |

> **Decision:**
> - **Ready items:** Use Storefront `cartCreate` → `checkoutUrl` (new Cart API). Need to add `CreateStorefrontCart()` to `shopify/client.go`.
> - **Pre-order items:** Backend creates a separate Shopify Draft Order (Admin API) AFTER the webhook fires — NOT at checkout time. Pre-order items ride the same checkout URL as ready items (same cart, mixed), and the backend separates them post-payment.

### Webhook vs Redirect Confirmation

| Method | How it works | Reliability |
|--------|-------------|-------------|
| Webhook (`orders/paid`) | Shopify POSTs to our `/webhooks/shopify/orders/paid` after payment | ✅ Reliable, async, survives browser close |
| Redirect callback (`?token=...`) | Shopify redirects buyer to a URL we configure | ⚠️ Unreliable — buyer may close browser |

> **Decision: Webhook is the primary confirmation path.** A redirect endpoint (`GET /checkout/confirm`) is added as a UX convenience (so the frontend can show an order-confirmed page), but it does NOT perform any business logic — it only looks up an order by Shopify order ID and redirects the customer.

---

## Architecture

```
Frontend
  └─ POST /api/v1/checkout
         │ { session_id, address, shipping_method }
         ▼
  Backend: CheckoutService
         │ 1. Fetch cart (existing CartService)
         │ 2. Build Shopify checkout line items
         │ 3. Call shopify.CreateStorefrontCheckout()
         │ 4. Return { checkout_url }
         ▼
  Frontend: window.location.href = checkout_url
         ▼
  Shopify Hosted Payment Page (buyer pays)
         ▼
  Shopify POSTs webhook → POST /webhooks/shopify/orders/paid
         │  { shopify_order_id, line_items, customer_email, ... }
         ▼
  Backend: WebhookHandler
         │ 1. Verify HMAC signature
         │ 2. Upsert customer by email
         │ 3. Create Order record in our DB
         │ 4. Deduct inventory (update product_variants.inventory_quantity)
         │ 5. Fire SendOrderConfirmation email
```

---

## Proposed Changes

### Part 1 — `POST /api/v1/checkout`

#### [MODIFY] `internal/checkout/handler.go`
- Add `POST /checkout` route
- Reads `session_id` from `X-Session-ID` header (guest) or user from Bearer token
- Calls new `CheckoutService.InitiateCheckout()`
- Returns `{ checkout_url: "https://..." }`

#### [NEW] `internal/checkout/service.go`
- `CheckoutService` interface with `InitiateCheckout(ctx, userID, sessionID *string, req) (*InitiateCheckoutResponse, error)`
- Implementation:
  - Calls `CartService.GetCartResponse()` to get line items
  - Maps cart items to `shopify.CheckoutLineItem` format
  - Calls `shopify.CreateStorefrontCheckout()`
  - Returns the `webUrl`

#### [MODIFY] `internal/checkout/dto.go`
- Add `InitiateCheckoutRequest` (address, shipping_method)
- Add `InitiateCheckoutResponse` (`checkout_url string`)

---

### Part 2 — Shopify Order Paid Webhook

#### [NEW] `internal/webhook/` (new module)
- `handler.go` — `POST /webhooks/shopify/orders/paid`
  - Reads raw body, verifies HMAC-SHA256 against `X-Shopify-Hmac-SHA256` header using `SHOPIFY_WEBHOOK_SECRET`
  - Parses Shopify order payload
  - Delegates to `WebhookService.HandleOrderPaid()`
- `service.go` — `WebhookService`
  - Upserts customer by email from payload
  - Creates `Order` record (maps Shopify order to our schema)
  - Updates `inventory_quantity` for each variant in the order
  - Calls `NotificationService.SendOrderConfirmation()`
- `dto.go` — Shopify order webhook payload struct (only the fields we consume)

#### [MODIFY] `cmd/server/main.go`
- Mount `/webhooks/shopify/orders/paid` (no auth middleware — Shopify cannot send Bearer tokens; security is via HMAC verification)
- Wire `WebhookService` with `OrderStore`, `AuthStore`, `ProductStore`, `NotificationService`

#### [MODIFY] `config/` — Add `SHOPIFY_WEBHOOK_SECRET` to config struct and `.env.template`

---

### Part 3 — UX Redirect (Optional, Stateless)

#### [MODIFY] `internal/checkout/handler.go`
- Add `GET /checkout/confirm?shopify_order_id=...`
- Looks up our DB for an order with `shopify_order_id`
- Returns `{ order_id, order_number, status }` so frontend can render an order-confirmed page
- **No business logic here** — pure read. If the order does not yet exist (webhook hasn't arrived), return `404` and let the frontend poll or retry.

---

### Part 4 — Product Sync Without Admin Login (Point 3 from Frontend)

> **Frontend note:** "Sync product tidak bisa otomatis dilakukan dari fetch frontend karena perlu login admin"

The existing `POST /api/v1/products/sync` requires admin JWT. This is correct for manual sync. The frontend's store page (guest-accessible) should never trigger a sync — it should just read from our local DB.

**No backend change needed.** The recommendation to the frontend team:
- Guest store page → `GET /api/v1/products` (no auth, reads our local DB)
- Product sync → triggered only by admin from the admin dashboard via `POST /api/v1/products/sync` (requires Bearer token)

This is already the correct design. Document it, no code change needed.

---

## New Config Variables

```
SHOPIFY_WEBHOOK_SECRET=   # From Shopify Partners > Webhooks > Signing secret
```

---

## Verification Plan

```bash
# Build check
go build ./...

# Manual E2E (local)
# 1. POST /api/v1/checkout with session_id from a cart with items
#    → Should return { checkout_url }
# 2. Simulate webhook: POST /webhooks/shopify/orders/paid with a test payload + valid HMAC
#    → Order should appear in our DB
#    → Inventory should decrease
#    → Email should appear in Mailpit (localhost:8025)
# 3. GET /checkout/confirm?shopify_order_id=<id>
#    → Should return our local order record
```

### HMAC Verification Testing
- Use Shopify's [webhook simulator](https://shopify.dev/docs/apps/webhooks/testing) or generate a test HMAC manually:
  ```bash
  echo -n '<raw_payload>' | openssl dgst -sha256 -hmac '<SHOPIFY_WEBHOOK_SECRET>' -binary | base64
  ```
- Send as `X-Shopify-Hmac-SHA256` header to the webhook endpoint

---

## Resolved Decisions (from PRD + User)

### Q1: `checkoutCreate` deprecation?
**Resolved:** Store is post-2024. **Use Storefront Cart API** (`cartCreate` → `checkoutUrl`). The `CreateStorefrontCheckout()` method in `shopify/client.go` must be replaced with a new `CreateStorefrontCart()` implementation. The service layer interface shields all callers from this change.

---

### Q2: Pre-order 50% deposit in Shopify checkout?

**Resolved via PRD.** The PRD explicitly defines this:

> *Initial payment = 100% ready + 50% preorder + ship #1 + tax*
> *All items go into one unified Shopify Cart. Customer pays the blended amount via Shopify Payments.*

The mixed cart (ready + pre-order) goes into one Shopify checkout. The 50% deposit is achieved by: **the pre-order line items in the cart use 50% of the actual price.** This requires creating a line item with a custom price on the Shopify Cart (Storefront Cart API supports `price` override per line item via `customAttributes` or by passing a custom selling price).

After payment (`orders/create` webhook):
- Ready items → stay in the Shopify Order, marked as PAID, queued for Shipment 1
- Pre-order items → backend creates a **separate Shopify Draft Order** (Admin API `draftOrderCreate`) tagged with `parent_order_id`, `dp_paid_amount`. Status: `open`, representing the outstanding 50% balance (pelunasan)

---

### Q3: Webhook event — `orders/paid` or `orders/create`?

**Resolved via PRD:**

> *Webhook: `orders/create` → backend identifies line items by tag/metafield*

**Use `orders/create`.** This fires for ALL orders regardless of `financial_status`. This is correct because:
- At checkout, Shopify Payments processes the mixed amount (ready 100% + pre-order 50%)
- The order `financial_status` may be `paid` (all items fully paid) or `partial` (pre-order deposit only)
- `orders/paid` only fires when `financial_status` becomes `paid`, which does not cover the pre-order deposit scenario
- `orders/create` fires immediately after Shopify creates the order — we then read `financial_status` and line items to determine the split

---

## Part 5 — Fix: Dynamic `fulfillment_type` Based on Inventory

**Frontend feedback:** `fulfillment_type` is hardcoded to `"ship_ready"` during product sync. It must be dynamic.

**PRD rule (Section 6):**
> *Pre-order does not consume current inventory — only batch quota.*
> *Source of truth: Shopify Inventory API.*

**Fix: Derive `fulfillment_type` from `inventory_quantity` at response time, not at sync time.**

| inventory_quantity | fulfillment_type |
|---|---|
| > 0 | `ship_ready` |
| ≤ 0 | `pre_order` |

#### [MODIFY] `internal/product/service.go`
- In `mapVariantToDTO()` (line ~100): replace `FulfillmentType(variant.FulfillmentType)` with a helper:
  ```go
  // fulfillmentTypeFromQty derives fulfillment type from local inventory.
  func fulfillmentTypeFromQty(qty int) FulfillmentType {
      if qty > 0 {
          return FulfillmentTypeShipReady
      }
      return FulfillmentTypePreOrder
  }
  ```
- The `fulfillment_type` column in `product_variants` DB table is **no longer the source of truth for the API response** — it can be kept for the filter query but the response value is always derived from `inventory_quantity`.

> **Note:** During sync (`SyncProducts`), line 202 still writes `FulfillmentTypeShipReady` to the DB. That column can remain as a default/fallback for the DB-level filter query (`?fulfillment_type=pre_order`). However, to make the filter also accurate, the sync should also derive and store the correct value from `inventory_quantity`. Update line 202 to use `fulfillmentTypeFromQty(vNode.InventoryQuantity)` as well.

---

## Part 6 — Inventory Freshness: Keeping `fulfillment_type` Accurate Automatically

**Problem:** `inventory_quantity` (and therefore `fulfillment_type`) only updates when an admin manually triggers `POST /api/v1/products/sync`. Between syncs, a sold-out variant can appear as `ship_ready` to store visitors.

**Solution: Two-layer approach**

| Layer | Mechanism | Latency | Scope |
|-------|-----------|---------|-------|
| **Primary** | Shopify `inventory_levels/update` webhook | Real-time (~seconds) | Only the changed variant |
| **Secondary** | Nightly scheduled reconciliation sync | ~24h max drift | All products (safety net) |

---

### Layer 1 — `inventory_levels/update` Webhook

Shopify fires this event whenever inventory changes for any variant (a sale completes, admin adjusts stock, a return is processed, etc.).

#### [MODIFY] `internal/webhook/handler.go`
- Register `POST /webhooks/shopify/inventory_levels/update` alongside the existing `orders/create` handler
- Same HMAC verification applies
- Payload contains: `inventory_item_id`, `location_id`, `available` (the new quantity)

#### [MODIFY] `internal/webhook/service.go`
- Add `HandleInventoryUpdate(ctx, payload InventoryUpdatePayload) error`
- Look up `product_variants` by `shopify_variant_id` (need to map from `inventory_item_id` — Shopify's `inventory_item_id` is 1:1 with a variant; store it during sync)
- Update `inventory_quantity = payload.Available`
- Update `fulfillment_type = fulfillmentTypeFromQty(payload.Available)` in the same DB row

#### [MODIFY] `internal/product/store.go`
- Add `UpdateVariantInventory(ctx, shopifyInventoryItemID string, qty int, fulfillmentType string) error`

#### [MODIFY] `internal/product/store.go` (model)
- Ensure `ProductVariant` model stores `ShopifyInventoryItemID string` — this field must be persisted during sync so the webhook handler can look up the right variant

#### [MODIFY] `internal/product/service.go` (`SyncProducts`)
- During sync, persist `vNode.InventoryItem.ID` into `shopify_inventory_item_id` column

#### [NEW] Migration `013_add_shopify_inventory_item_id.sql`
```sql
ALTER TABLE product_variants
  ADD COLUMN IF NOT EXISTS shopify_inventory_item_id VARCHAR(255);
CREATE INDEX IF NOT EXISTS idx_variants_inventory_item_id
  ON product_variants(shopify_inventory_item_id);
```

---

### Layer 2 — Nightly Reconciliation Sync

The webhook can fail silently (network blip, Shopify retry exhausted). A nightly sync catches any drift.

#### [MODIFY] `internal/platform/scheduler/scheduler.go`
- Existing scheduler already calls `preorderService.ProcessReminders(ctx)` daily
- Add a second daily job: `productService.SyncProducts(ctx)`
- Run at a fixed off-peak time (e.g., 02:00 UTC)

#### [MODIFY] `cmd/server/main.go`
- Pass `productService` to the scheduler so it can call `SyncProducts` nightly

> **Note:** Nightly sync fetches ALL products from Shopify (existing `SyncProducts` logic). This is acceptable since it runs once per day at low traffic. At very high product counts (10,000+) it would need pagination batching, but that is a future concern.

---

### New Config Variable

```
# Already exists — no new env vars needed for this feature
# SHOPIFY_WEBHOOK_SECRET is shared across all webhook event types
```

---

### Verification

```bash
# Simulate inventory_levels/update webhook:
curl -X POST http://localhost:3000/webhooks/shopify/inventory_levels/update \
  -H "Content-Type: application/json" \
  -H "X-Shopify-Hmac-SHA256: <computed_hmac>" \
  -d '{"inventory_item_id":"gid://shopify/InventoryItem/123","available":0}'

# Verify variant in DB:
# SELECT fulfillment_type, inventory_quantity FROM product_variants WHERE shopify_inventory_item_id = '...';
# Expected: fulfillment_type = 'pre_order', inventory_quantity = 0

# Verify API response:
# GET /api/v1/products → variant should show fulfillment_type: "pre_order"
```
