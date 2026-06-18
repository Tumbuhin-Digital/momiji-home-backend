# End-to-End Test Plan & Feature Audit
*2026-06-18*

---

## Feature Audit: What's Implemented vs. Missing

> **Auth model:** `POST /auth/login` and related endpoints are **admin-only**.
> Customers shop anonymously as guests using `X-Session-ID` (from `POST /cart/session`).

### ✅ Fully Implemented

| Area | Endpoints |
|---|---|
| **Admin Auth** | POST /auth/login (admin only), POST /auth/refresh, POST /auth/logout |
| **Cart** | POST /cart/session, GET /cart, GET /cart/summary, POST /cart/items, PATCH /cart/items/variant, PATCH /cart/items/:id, DELETE /cart/items/:id, DELETE /cart, POST /cart/merge |
| **Shipping** | POST /shipping/validate-address, POST /shipping/rates (ShipStation live) |
| **Checkout** | POST /checkout/summary, POST /checkout, GET /checkout/confirm |
| **Orders** | POST /orders, GET /orders, GET /orders/:id, PATCH /orders/:id/accept, PATCH /orders/:id/cancel, PATCH /orders/:id/items/:itemId/step, PATCH /orders/:id/items/:itemId/received, PATCH /orders/:id/items/:itemId/tracking, GET /orders/export |
| **Preorder Settlements** | GET /preorders, GET /preorders/export, GET /preorders/settlements/:id, PATCH /preorders/settlements/:id/invoice, PATCH /preorders/settlements/:id/paid |
| **Products** | GET /products, GET /products/:id, POST /products/sync, PATCH /products/variant/price, PATCH /products/variant/dimensions/bulk-upload, PATCH /products/:id/status, PATCH /products/:id/batch-label |
| **Webhooks** | POST /webhooks/shopify/orders/paid, POST /webhooks/shopify/orders/cancelled, POST /webhooks/shopify/products/update, POST /webhooks/shopify/inventory/update, POST /webhooks/shopify/draft_orders/delete |
| **Dashboard** | GET /dashboard/summary |
| **Customers** | GET /customers, GET /customers/:id |

### ⚠️ Gaps Identified

| # | Gap | Severity | Notes |
|---|---|---|---|
| 1 | **AcceptOrder → push to Shopify** not implemented. Ship-ready orders accepted by admin should create a Shopify `fulfillmentCreateV2` to appear under "Ready to fulfill" in Shopify dashboard. | HIGH | Planned but not coded yet. |
| 2 | **`GET /checkout/confirm` race condition** — FE polls with `checkout_reference` UUID, but our webhook sets `shopify_draft_order_id`. Confirm the lookup chain correctly resolves the draft order ID → real order after webhook fires. | HIGH | 404 response is already polling-friendly; FE must retry. |
| 3 | **Order confirmation email missing** — Webhook creates order but no "thank you for your order" email is sent to the customer. Only tracking email fires later. | MEDIUM | |
| 4 | **`/checkout/summary` shipping cost is hardcoded** ($20 ground / $35 expedited). Should accept `service_code` from ShipStation and show real cost. | MEDIUM | Low impact if FE reads cost from `/shipping/rates` directly. |
| 5 | **Settlement emails use hardcoded `"Customer"` name** instead of real customer name from the order. | LOW | |
| 6 | **`FE_URL` missing from `.env.template`** — env var exists in config but template doesn't document it. | LOW | |

---

## E2E Test Scenarios

### Scenario A: Ship-Ready Flow (Customer buys in-stock item)

```
1.  [GUEST]   POST /cart/session
               → session_id

2.  [GUEST]   POST /cart/items
               { variant_id: "gid://shopify/ProductVariant/...", quantity: 1 }
               [item with inventory_quantity > 0 and fulfillment_type = "ship_ready"]
               → 200 OK

3.  [GUEST]   GET /cart
               → ship_ready: [{ ... }], pre_order: []

4.  [GUEST]   POST /shipping/validate-address
               { country: "US", state: "NY", city: "New York", zip: "10001" }
               → 200 OK (valid)

5.  [GUEST]   POST /checkout
               { email: "test@example.com", first_name: "...", address1: "...", city: "...", state: "NY", zip: "10001", country: "US" }
               → { checkout_url, checkout_reference }

6.  [MANUAL]  Customer pays on Shopify checkout page (use Shopify test payment)
               → Shopify fires POST /webhooks/shopify/orders/paid

7.  [POLL]    GET /checkout/confirm?checkout_reference={checkout_reference}
               Retry until 200 (webhook may take 2-5 seconds)
               → { order_id, order_number, customer_email, items, financial_status: "paid" }
               ✅ EXPECT: confirmation email sent to customer

8.  [ADMIN]   GET /orders (role=admin)
               → order appears with status: "processing"

9.  [ADMIN]   PATCH /orders/:id/accept
               { fulfillment_type: "ship_ready" }
               → 200 OK
               ✅ EXPECT: order status → "on_progress"
               ❌ MISSING: Shopify fulfillment not created yet (Gap #2)

10. [ADMIN]   PATCH /orders/:id/items/:itemId/step
               { fulfillment_step: 2 }
               → 200 OK

11. [ADMIN]   PATCH /orders/:id/items/:itemId/tracking
               { tracking_number: "1Z999AA...", tracking_url: "https://ups.com/track?..." }
               → 200 OK
               ✅ EXPECT: shipment dispatched email sent to customer

12. [ADMIN]   PATCH /orders/:id/items/:itemId/received
               { items_received: 1 }
               → 200 OK
```

---

### Scenario B: Pre-Order Flow (Customer buys pre-order item)

> **Customer auth model:** Customers are always guests — no login required.
> All customer requests use `X-Session-ID: {session_id}` header (obtained from `POST /cart/session`).
> Login (`POST /auth/login`) is **admin-only**.

```
1.  [GUEST]   POST /cart/session
               → { session_id }  (save this, use as X-Session-ID header for all following steps)

2.  [GUEST]   POST /cart/items  (X-Session-ID header)
               { variant_id: "...", quantity: 2 }
               [item with fulfillment_type = "pre_order"]
               → 200 OK

3.  [GUEST]   GET /cart  (X-Session-ID header)
               → pre_order: [{ deposit_amount: "...", balance_due: "..." }]

4.  [GUEST]   POST /shipping/validate-address  (X-Session-ID header)
               { country: "US", state: "CA", city: "Oakland", zip: "94602" }
               → 200 OK

5.  [GUEST]   POST /shipping/rates  (X-Session-ID header)
               { name: "Jane Doe", address1: "4361 Bridgeview Dr", city: "Oakland", state: "CA", zip: "94602", country: "US" }
               → [ { service_code, label, cost, currency, delivery_days }, ... ]

6.  [GUEST]   POST /checkout  (X-Session-ID header)
               { email: "jane@example.com", shipping_method: "ups_ground",
                 first_name: "Jane", last_name: "Doe",
                 address1: "4361 Bridgeview Dr", city: "Oakland", state: "CA",
                 zip: "94602", country: "US" }
               → { checkout_url, checkout_reference }
               NOTE: email is REQUIRED for guests (used by webhook to link order)

7.  [MANUAL]  Customer pays 50% deposit on Shopify checkout page (test gateway)
               → Shopify fires POST /webhooks/shopify/orders/paid
               ✅ EXPECT: preorder settlement record created with status: "pending"

8.  [POLL]    GET /checkout/confirm?checkout_reference={checkout_reference}  (X-Session-ID)
               Retry until 200 (webhook fires async, may take 2-5 seconds)
               → { order_id, order_number, financial_status: "paid",
                   customer_email: "jane@example.com",
                   items: [{ type: "pre_order", ... }] }

9.  [ADMIN]   GET /preorders  (Bearer token)
               → [ { product_name: "...", total_quantity: 2,
                     settlements: [{ settlement_id, order_number, settlement_status: "pending" }] } ]

10. [ADMIN]   PATCH /orders/:id/accept  (Bearer token)
               { fulfillment_type: "pre_order" }
               → 200 OK  (order status → "on_progress")

11. [ADMIN]   PATCH /preorders/settlements/:id/invoice  (Bearer token)
               → 200 OK (settlement status: "invoiced")
               ✅ EXPECT: invoice email sent to jane@example.com with payment link

12. [CUSTOMER] Customer receives email → pays balance via link (Shopify payment page)
               NOTE: no backend action needed here — payment is Shopify-handled

13. [ADMIN]   PATCH /preorders/settlements/:id/paid  (Bearer token)
               → 200 OK (settlement status: "paid")
               ✅ EXPECT: payment confirmation email sent to customer
               ✅ EXPECT: if all settlements for this order are paid → order.aggregate_status = "paid"

14. [ADMIN]   PATCH /orders/:id/items/:itemId/tracking  (Bearer token)
               { tracking_number: "1Z999AA...", tracking_url: "https://ups.com/..." }
               → 200 OK
               ✅ EXPECT: shipment dispatched email sent to customer
```

---

### Scenario C: Mixed Cart (Ship-Ready + Pre-Order in same checkout)

```
1-3. Same cart setup as above, but add BOTH a ship_ready AND a pre_order item.
4.   POST /shipping/rates  (only pre_order items trigger ShipStation call)
5.   POST /checkout → creates draft order with both line items
6.   Payment webhook → creates ONE order with two item types
7.   GET /checkout/confirm → items[] contains both types
8.   Admin sees order; must accept each part independently:
       PATCH /orders/:id/accept { fulfillment_type: "ship_ready" }
       PATCH /orders/:id/accept { fulfillment_type: "pre_order" }  ← CHECK: is this valid?
```

---

### Scenario D: Address Validation Failure

```
1.  POST /shipping/validate-address
    { country: "US", state: "CA", city: "New York", zip: "10001" }  ← Wrong state for ZIP
    → 422 Unprocessable Entity
    { errors: { city: "City does not match ZIP", state: "State does not match ZIP" } }
```

---

### Scenario E: Pre-Order Settlement Reminder (Scheduled)

```
1.  Settlement invoiced D+3 days ago
    → Scheduler fires daily → ProcessReminders → D+3 reminder email sent
2.  Settlement invoiced D+6 days ago → D+6 final reminder
3.  Settlement invoiced D+7 days ago → auto-expire + 80% refund to Shopify + expiry email
```

---

### Scenario F: Admin Auth + Token Refresh Flow

> **Note:** Login is for admin users only. Customers shop as guests using X-Session-ID.

```
1.  [ADMIN]   POST /auth/login
               { email: "admin@momiji.com", password: "..." }
               → { access_token } + HttpOnly cookie: refresh_token

2.  [ADMIN]   15 minutes later → access_token expires
               Any admin request returns 401 Unauthorized

3.  [ADMIN]   POST /auth/refresh
               (refresh_token cookie sent automatically)
               → { access_token }  (new token, valid 15 min)

4.  [ADMIN]   Retry the original request with new access_token → success

5.  [ADMIN]   POST /auth/logout
               → 200 OK, cookie cleared
```

---

## Summary of Gaps to Fix Before Full E2E

| Priority | Action |
|---|---|
| 🔴 HIGH | Implement `AcceptOrder` → Shopify fulfillment push (`CreateFulfillment` in Shopify client) |
| 🔴 HIGH | Confirm `GET /checkout/confirm` lookup correctly uses `checkout_reference` UUID to find order after webhook |
| 🟡 MEDIUM | Order confirmation email (currently missing — webhook creates order but no email fires) |
| 🟡 MEDIUM | `/checkout/summary` should accept `service_code` from ShipStation and return real cost |
| 🟢 LOW | Fix settlement email customer name (currently hardcoded "Customer") |
| 🟢 LOW | Add `FE_URL` to `.env.template` |
