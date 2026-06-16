# Plan: Cart Variant-Quantity Endpoint
*Based on FE team request in `cart-add-item-flow-analysis.md` — 2026-06-16*

## Problem Statement

The FE team has identified that the **split logic** (deciding how many units are `ship_ready` vs `pre_order` for a single variant) currently lives on the **frontend**, requiring up to **2 parallel API calls** per quantity change. This is:
- **Fragile**: Race conditions possible when both calls succeed/fail independently.
- **Wrong layer**: Business logic (how inventory splits) belongs in the backend.
- **Redundant**: The FE has to re-derive stock from `GET /cart` before it can even know what IDs to PATCH.

## Decision: Option A — `PATCH /cart/items/variant/:variant_id`

We go with **Option A** (the ideal approach recommended by FE), NOT Option B.

**Why:** The backend already holds the source of truth for `inventory_quantity` (it's on the `product_variants` table, not a Shopify live call). We already use it in `AddItem`. We simply extend this logic to handle `total_quantity` as a full reconciliation (upsert + delete), run it in **a single DB transaction**, and return 200.

---

## How the New Endpoint Works

```
PATCH /cart/items/variant/:variant_id
Body: { "total_quantity": 12 }
```

Backend logic:
1. Fetch variant from DB → get `inventory_quantity` and `fulfillment_type`.
2. Compute the split:
   - If `fulfillment_type = "pre_order"` (admin-forced) → ALL units go to `pre_order`
   - Otherwise: `ship_ready = min(total, inventory_quantity)`, `pre_order = max(0, total - inventory_quantity)`
3. Fetch existing cart items for this `variant_id` in the user's cart (may be 0, 1, or 2 rows).
4. In a **single transaction**:
   - For `ship_ready`: if qty > 0 → upsert, else → delete row if exists
   - For `pre_order`: if qty > 0 → upsert, else → delete row if exists
5. Return `200 OK`

---

## Proposed Changes

### 1. DTO

#### [MODIFY] `internal/cart/dto.go`
Add the new request struct:
```go
type SetVariantQuantityRequest struct {
    TotalQuantity int `json:"total_quantity" validate:"required,min=0"`
}
```
> `min=0` allows setting to 0 (which removes all items for that variant).

---

### 2. Store Layer

#### [MODIFY] `internal/cart/store.go`
Add two new methods to `CartStore` interface:

```go
// GetVariantItemsInCart returns all cart_items rows for a given variant_id in the cart.
// A variant can have up to 2 rows: one ship_ready, one pre_order.
GetVariantItemsInCart(ctx context.Context, cartID string, variantID string) ([]CartItemModel, error)

// UpsertVariantItems atomically sets the ship_ready and pre_order quantities
// for a variant. It creates, updates, or deletes rows within a single transaction.
UpsertVariantItems(ctx context.Context, cartID string, variantID string, shipReadyQty int, preOrderQty int, unitPrice float64) error
```

#### [MODIFY] `internal/cart/postgres.go`
Implement `UpsertVariantItems`:
```go
func (s *PostgresStore) UpsertVariantItems(ctx context.Context, cartID, variantID string, shipReadyQty, preOrderQty int, unitPrice float64) error {
    return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        types := []struct {
            ft  string
            qty int
        }{
            {"ship_ready", shipReadyQty},
            {"pre_order", preOrderQty},
        }

        for _, t := range types {
            var existing CartItemModel
            err := tx.Where("cart_id = ? AND shopify_variant_id = ? AND fulfillment_type = ?",
                cartID, variantID, t.ft).First(&existing).Error

            if t.qty == 0 {
                // Delete if exists
                if err == nil {
                    tx.Delete(&existing)
                }
            } else if errors.Is(err, gorm.ErrRecordNotFound) {
                // Insert new
                tx.Create(&CartItemModel{
                    CartID: cartID, ShopifyVariantID: variantID,
                    FulfillmentType: t.ft, Quantity: t.qty, UnitPrice: unitPrice,
                })
            } else {
                // Update existing
                tx.Model(&existing).Update("quantity", t.qty)
            }
        }
        return nil
    })
}
```

---

### 3. Service Layer

#### [MODIFY] `internal/cart/service.go`
Add `SetVariantQuantity` to the `CartService` interface and implement it:

```go
// Interface addition:
SetVariantQuantity(ctx context.Context, userID, sessionID *string, variantID string, totalQty int) error

// Implementation:
func (s *service) SetVariantQuantity(ctx context.Context, userID, sessionID *string, variantID string, totalQty int) error {
    variant, err := s.productService.GetVariantByID(ctx, variantID)
    if err != nil {
        return apierror.ErrNotFound
    }

    cart, err := s.getOrCreateCart(ctx, userID, sessionID)
    if err != nil {
        return err
    }

    var shipReadyQty, preOrderQty int

    if totalQty == 0 {
        shipReadyQty = 0
        preOrderQty = 0
    } else if string(variant.FulfillmentType) == string(product.FulfillmentTypePreOrder) {
        // Admin has force-flagged this as pre_order — ALL units go to pre_order
        shipReadyQty = 0
        preOrderQty = totalQty
    } else {
        shipReadyQty = min(totalQty, variant.InventoryQuantity)
        preOrderQty = max(0, totalQty - variant.InventoryQuantity)
    }

    var price float64
    fmt.Sscanf(variant.WSPrice, "%f", &price)

    return s.store.UpsertVariantItems(ctx, cart.ID, variantID, shipReadyQty, preOrderQty, price)
}
```

---

### 4. Handler

#### [MODIFY] `internal/cart/handler.go`
Register and implement the new route:

```go
// In SetupRoutes, add alongside other optAuth routes:
optAuth.Patch("/items/variant/:variant_id", h.SetVariantQuantity)

// Handler:
// @Summary Set total quantity for a variant (auto-splits ship_ready vs pre_order)
// @Tags Cart
// @Accept json
// @Param variant_id path string true "Shopify Variant GID"
// @Param request body SetVariantQuantityRequest true "Total Quantity"
// @Success 200 {object} response.Envelope
// @Router /cart/items/variant/{variant_id} [patch]
func (h *Handler) SetVariantQuantity(c *fiber.Ctx) error {
    uid, sid := h.extractAuth(c)
    variantID := c.Params("variant_id")

    var req SetVariantQuantityRequest
    if err := c.BodyParser(&req); err != nil {
        return response.Error(c, err)
    }
    if err := validator.ValidateStruct(&req); err != nil {
        return response.Error(c, err)
    }

    if err := h.service.SetVariantQuantity(c.Context(), uid, sid, variantID, req.TotalQuantity); err != nil {
        return response.Error(c, err)
    }
    return response.Success(c, fiber.StatusOK, "Cart updated", nil)
}
```

> **Note on routing:** Register `Patch("/items/variant/:variant_id", ...)` BEFORE `Patch("/items/:id", ...)` in the route group. Fiber matches routes in registration order, and `:id` would capture "variant" as a wildcard otherwise.

---

## Impact on Existing Endpoints

The existing endpoints (`POST /cart/items`, `PATCH /cart/items/:id`) are **NOT removed**. They remain backward compatible. The FE team can migrate to the new endpoint at their own pace.

---

## What FE Needs to Do

Replace the `triggerUpdate` logic which currently does up to 2 parallel calls, with a single call to:

```typescript
// Instead of:
await Promise.all([
  updateShipReady(shipReadyId, shipReadyQty),
  addOrUpdatePreOrder(variantId, preOrderQty)
])

// Do:
await apiClient.patch(`/cart/items/variant/${encodeURIComponent(variantId)}`, {
  total_quantity: newTotal
})
```

Also, the `min=0` validation means **removing all items for a variant** is just:
```typescript
await apiClient.patch(`/cart/items/variant/${encodeURIComponent(variantId)}`, {
  total_quantity: 0
})
```

---

## Verification Plan

1. Add item (qty 5, stock = 3) → expect `ship_ready: 3, pre_order: 2`
2. Reduce to qty 2 → expect `ship_ready: 2, pre_order: 0` (pre_order row deleted)
3. Set qty 0 → expect both rows deleted
4. For admin-flagged `pre_order` variant (stock > 0) → ALL units go to `pre_order`
5. Concurrent PATCH calls with same variant → no partial state due to DB transaction
