# Cart UX Optimization & Fulfillment Type Auto-Derivation

> **Status:** 🔲 PENDING
> **Date:** 2026-06-04
> **Triggered by:** Frontend team brainstorm session
> **Pre-requisite:** Phase 9 Part 5 (`fulfillmentTypeFromQty` helper) must be implemented first

---

## Background

Two issues were identified in the current cart flow during a PO + FE alignment session:

1. **Cart API call pattern is inefficient** — frontend calls `POST /api/v1/cart/items` on every single `+` button click. On rapid clicks, this fires N sequential API calls for the same variant.

2. **`fulfillment_type` is decided by the frontend** — the current contract requires the client to send `fulfillment_type: "ship_ready" | "pre_order"` in the request body. The backend then validates that the client's value matches the DB. This means:
   - The frontend must know the current inventory state to determine the type
   - On the "7th click" (when stock runs out), the frontend must detect the switch and change the field it sends
   - Any desync between frontend's cached inventory and the real DB leads to a 400 error
   - This is a fragile, client-decides pattern that violates the principle that the backend is the source of truth

---

## Decision 1 — Cart Add UX: Debounce + Absolute Quantity

### Problem

Frontend currently calls `POST /api/v1/cart/items` (delta: "add N units") on each `+` button click. Rapid clicks = N API calls.

### Race Condition Analysis

| Scenario | Risk Level | Explanation |
|----------|-----------|-------------|
| Delta calls in rapid succession | **Medium** | Two debounced `POST` calls could fire if debounce window is too short, adding item twice |
| Absolute `PATCH` with debounce | **None** | Both calls write the same final quantity — idempotent |
| Inventory race between two users | **By design** | PRD: inventory NOT decremented at cart-add time. Conflict resolves at checkout (15-min stock lock). First-come-first-served |

### Recommended Pattern

```
First + click (new item, not yet in cart)
  └─ POST /api/v1/cart/items { variant_id, quantity: 1 }
     (one call, fires immediately — new item creation)

Subsequent + clicks (item already in cart)
  └─ Local state: increment desired quantity
  └─ Debounce 600–800ms
  └─ PATCH /api/v1/cart/items/:id { quantity: <absolute total> }
     (idempotent — if two calls fire, both write same value)

- click (decrement)
  └─ Same debounce pattern as above
  └─ If quantity reaches 0 → DELETE /api/v1/cart/items/:id
```

### Why Absolute Quantity (Not Delta) Matters

- **Delta** (`"add 1"`) → two fired calls = item added twice ❌
- **Absolute** (`"set to 4"`) → two fired calls = both write `4` ✅

The `PATCH /api/v1/cart/items/:id` endpoint already accepts an absolute `quantity` field. **No backend change required for this decision.**

---

## Decision 2 — `fulfillment_type` Must Be Derived by Backend, Not Sent by Client

### Problem

Current contract:
```json
POST /api/v1/cart/items
{ "variant_id": 48513249149183, "fulfillment_type": "ship_ready", "quantity": 1 }
```

Current backend validation in `internal/cart/service.go`:
```go
if string(variant.FulfillmentType) != req.FulfillmentType {
    return apierror.ErrBadRequest
}
```

**Client decides type → fragile.** The "7th click" scenario fails if inventory changes between when the FE last fetched product data and when the API call fires.

### The "7th Click" Scenario

> Product has `inventory_quantity = 6`. User clicks `+` 6 times. The 7th click should add as `pre_order`.

This requires the backend to account for **units already in the cart**:

```
available = inventory_quantity - quantity_already_in_cart_for_this_variant

available > 0  →  fulfill as "ship_ready"
available ≤ 0  →  fulfill as "pre_order"
```

Simply checking `inventory_quantity > 0` is not sufficient — cart adds don't deplete Shopify inventory. The backend must cross-reference the current cart state.

### New Contract

Remove `fulfillment_type` from request body. Backend derives it internally.

```json
POST /api/v1/cart/items
{ "variant_id": 48513249149183, "quantity": 1 }
```

The field `fulfillment_type` remains in the **response** (on `GET /cart` and `POST /cart/items` response) so the frontend can render correct labels and group items. It is just no longer an **input**.

---

## Proposed Backend Changes

### Part A — `internal/cart/dto.go`

#### [MODIFY] `CartItemRequest`
- Remove `FulfillmentType string` field from request struct
- The field stays in response types (`CartItem`, etc.)

---

### Part B — `internal/cart/store.go` + `internal/cart/postgres.go`

#### [NEW] Store method: `GetVariantQtyInCart`
```
GetVariantQtyInCart(ctx, cartID string, shopifyVariantID string) (int, error)
```
Returns the total quantity of a specific variant already present in the given cart.
Simple `SUM(quantity)` query on `cart_items` filtered by `cart_id` and `shopify_variant_id`.

---

### Part C — `internal/cart/service.go`

#### [MODIFY] `AddItem`

Replace the current client-trust validation:
```go
// REMOVE:
if string(variant.FulfillmentType) != req.FulfillmentType {
    return apierror.ErrBadRequest
}
```

Replace with backend-derived logic:
```
1. Look up variant from product service → get InventoryQuantity
2. Look up how many units of this variant are already in the cart
   → call store.GetVariantQtyInCart(ctx, cart.ID, req.VariantID)
3. available = variant.InventoryQuantity - currentCartQty
4. if available > 0  → derivedType = "ship_ready"
   else              → derivedType = "pre_order"
5. Store derivedType (not req.FulfillmentType) in the cart item
```

#### Resulting behavior table

| inventory_quantity | already in cart | adding | derived type |
|-------------------|----------------|--------|-------------|
| 6 | 0 | 1 | ship_ready |
| 6 | 5 | 1 | ship_ready (1 left) |
| 6 | 6 | 1 | pre_order (none left) |
| 0 | 0 | 1 | pre_order |
| 0 | 3 | 1 | pre_order |

---

### Part D — `api_contract/Add to Cart *.md`

#### [MODIFY] Contract file
- Remove `fulfillment_type` from Request Body table
- Add note: *"fulfillment_type is derived by the backend based on current inventory and existing cart quantity. It is returned in the response but must not be sent in the request."*
- Keep `fulfillment_type` in the Success Response example

---

## No Migration Required

This is a service-layer logic change only. No DB schema change is needed. The `cart_items.fulfillment_type` column stays — it just gets written by the backend, not echoed from the client.

---

## Verification

```bash
# 1. Product with inventory=2, cart empty → first add
POST /api/v1/cart/items { "variant_id": "...", "quantity": 1 }
# Expected: fulfillment_type: "ship_ready" in response

# 2. Same product, cart already has 2 → third add
POST /api/v1/cart/items { "variant_id": "...", "quantity": 1 }
# Expected: fulfillment_type: "pre_order" in response

# 3. Product with inventory=0 → add
POST /api/v1/cart/items { "variant_id": "...", "quantity": 1 }
# Expected: fulfillment_type: "pre_order"

# 4. Sending fulfillment_type in request body should be ignored (not cause error)
POST /api/v1/cart/items { "variant_id": "...", "quantity": 1, "fulfillment_type": "ship_ready" }
# Expected: succeeds, backend ignores the sent field, derives its own
```

---

## Summary for Frontend Team

> Read this section if you are the frontend implementer.

### What the backend is changing

| Before | After |
|--------|-------|
| `POST /cart/items` requires `fulfillment_type` field | `POST /cart/items` — `fulfillment_type` field is **removed from request** |
| Frontend must decide `ship_ready` vs `pre_order` | Backend decides automatically from inventory + cart state |
| Sending wrong `fulfillment_type` → 400 error | No such error anymore |

### What the frontend must change

**1. Remove `fulfillment_type` from `POST /api/v1/cart/items` payload**

```js
// BEFORE (remove this)
const body = { variant_id, fulfillment_type: "ship_ready", quantity: 1 }

// AFTER (correct)
const body = { variant_id, quantity: 1 }
```

**2. Adopt debounce + absolute quantity pattern for the `+` / `-` buttons**

```
First + click on a new item → POST /cart/items (single call, no debounce needed)

Subsequent + or - on existing item:
  1. Update quantity in local state immediately (optimistic UI)
  2. Debounce 600–800ms
  3. Fire PATCH /cart/items/:id { quantity: <absolute final value> }
  4. If quantity reaches 0 → DELETE /cart/items/:id instead
```

**3. Do NOT derive `fulfillment_type` on the frontend for cart purposes**

- The `fulfillment_type` shown in the cart UI should come from **`GET /cart` response** only
- The `+` button does not need to know or track whether an item is ship_ready or pre_order — the backend handles it
- The only place FE needs to display the type is the cart page (from the API response) and checkout summary

**4. No other contract changes**

- `GET /cart` response shape is unchanged
- `PATCH /cart/items/:id` is unchanged
- `DELETE /cart/items/:id` is unchanged
- `GET /cart/summary` is unchanged
