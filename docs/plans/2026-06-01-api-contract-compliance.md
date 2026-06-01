# API Contract Compliance — Fix Plan

> **Status:** 🔲 PENDING
> **Date:** 2026-06-01
> **Source of truth:** `api_contract/` directory (39 files)
> **Pre-requisite:** Phase 8 (Email) in progress, Phase 9 (Checkout) planned separately in `2026-06-01-shopify-checkout-flow.md`

---

## Background

A full audit was done against all 39 API contract files in `api_contract/`. Several gaps were found between what the contracts define and what is currently implemented. This plan covers the pre-Phase-9 fixes that must be done to bring the existing API into alignment.

Phase 9 endpoints (`POST /checkout`, webhooks, etc.) are tracked separately in `2026-06-01-shopify-checkout-flow.md` and are **not part of this plan**.

---

## Priority 1 — Critical Field Mismatches (break frontend)

### Fix 1A — `PATCH /api/v1/products/:id/status` body field name

**Contract says:** request body field is `fulfillment_type`
**Implementation has:** `status`

#### [MODIFY] `internal/product/handler.go` — `UpdateProductStatus`
- Change the body struct field from `Status string json:"status"` to `FulfillmentType string json:"fulfillment_type"`
- Pass `req.FulfillmentType` to `h.service.UpdateProductStatus()`
- Contract also expects the response to echo back the updated values, not `nil`. Return `data: { id, fulfillment_type, preorder_batch_label, expected_ship_date, updated_at }`.

---

### Fix 1B — `PATCH /api/v1/products/:id/batch` body field name + missing `expected_ship_date`

**Contract says:** body fields are `preorder_batch_label` (string) and `expected_ship_date` (date)
**Implementation has:** `batch_label` only, `expected_ship_date` not handled at all

#### [MODIFY] `internal/product/handler.go` — `UpdateVariantBatchLabel`
- Rename body field `batch_label` → `preorder_batch_label`
- Add `expected_ship_date` to the body struct (ISO date string)
- Pass both to service

#### [MODIFY] `internal/product/service.go` — `UpdateVariantBatchLabel`
- Add `expectedShipDate string` parameter

#### [MODIFY] `internal/product/postgres.go` — `UpdateVariantBatchLabel`
- Add `expected_ship_date` to the DB updates map

> Check whether `product_variants` table has an `expected_ship_date` column. If not, add migration `013_add_expected_ship_date.sql` (or combine with `013_add_shopify_inventory_item_id.sql` from Phase 9).

---

### Fix 1C — `GET /api/v1/products` — `sort` parameter unsafe + wrong mapping

**Problem:** The `sort` query param is passed **raw** to the GORM `Order()` call, which is a SQL injection risk and also doesn't map the contract's values to actual column names.

**Contract sort values:**
| Contract value | SQL ORDER BY |
|----------------|--------------|
| `price_asc` | `product_variants.ws_price ASC` |
| `price_desc` | `product_variants.ws_price DESC` |
| `name_asc` | `products.title ASC` |
| `created_at` | `products.created_at DESC` |

#### [MODIFY] `internal/product/postgres.go` — `GetProducts`
- Add a sort allowlist map before the `query.Order()` call
- If `q.Sort` is not in the allowlist, default to `products.created_at DESC`
- Example mapping:
  ```
  "price_asc"   → "product_variants.ws_price ASC"  (requires join if not already joined)
  "price_desc"  → "product_variants.ws_price DESC"
  "name_asc"    → "products.title ASC"
  "created_at"  → "products.created_at DESC"
  ```
- For `price_asc` / `price_desc`, a join on `product_variants` is needed (ensure no double-join with the `fulfillment_type` filter join)

---

### Fix 1D — `GET /api/v1/products` — `fulfillment_type` filter always returns empty

**Root cause:** `SyncFromShopify` hardcodes `FulfillmentTypeShipReady` for every variant. The DB column `product_variants.fulfillment_type` is always `ship_ready`, so `?fulfillment_type=pre_order` always returns empty results.

**This is Phase 9 Part 5.** See `2026-06-01-shopify-checkout-flow.md` Part 5 for the fix (`fulfillmentTypeFromQty()` helper).

---

## Priority 2 — `ProductDTO` Response Shape Gaps

The contract defines many fields on the product response that are not in the current `ProductDTO` / `VariantDTO` / `ProductImageDTO` structs.

### Fix 2A — Add missing fields to `ProductDTO`

#### [MODIFY] `internal/product/dto.go`

Add to `ProductDTO`:
- `Handle string json:"handle"`
- `Vendor string json:"vendor"`
- `ProductType string json:"product_type"`
- `Tags string json:"tags"`
- `PreorderBatchLabel *string json:"preorder_batch_label"`
- `ExpectedShipDate *string json:"expected_ship_date"`
- `BodyHTML string json:"body_html"` (only needed for the detail endpoint — can be omitted from list if bandwidth is a concern, but contract includes it in detail)

Add to `VariantDTO`:
- `SKU *string json:"sku"`

Add to `ProductImageDTO`:
- `Alt string json:"alt"`
- `Position int json:"position"`

> All these fields exist in the DB (synced from Shopify). Verify the `Product` and `ProductVariant` GORM models have the columns and add any missing ones.

#### [MODIFY] `internal/product/service.go` — `mapProductToDTO()` and `mapVariantToDTO()`
- Map the newly added fields from `Product` / `ProductVariant` / image models to their corresponding DTO fields

---

### Fix 2B — `GET /api/v1/products` response echoes `sort` and `search`

**Contract response includes:**
```json
{
  "data": {
    "page": 1,
    "limit": 10,
    "total": 17,
    "totalPages": 2,
    "sort": "created_at",
    "search": "",
    "products": [...]
  }
}
```

The `sort` and `search` values should be echoed back in the paginated response.

#### [MODIFY] `internal/product/handler.go` — `GetProducts`
- After parsing query, include `sort` and `search` values in the `PaginatedData` response (or build a custom response envelope)
- Check if `response.PaginatedData` struct supports extra fields; if not, use a product-specific response struct

---

### Fix 2C — `GET /api/v1/products/:id/variants` — response wrapper shape

**Contract expects:**
```json
{
  "data": {
    "product_id": 9493716599039,
    "variants": [...]
  }
}
```

**Implementation returns:** flat `data: [...]` (just the variants array)

#### [MODIFY] `internal/product/handler.go` — `GetProductVariants`
- Wrap the variants in an object: `{ "product_id": <id>, "variants": <variants> }`
- Product ID comes from `c.Params("id")`

---

### Fix 2D — `limit` max enforcement

**Contract says:** max 100.

#### [MODIFY] `internal/product/handler.go` — `GetProducts`
- After parsing query, cap `query.Limit` at 100 if it exceeds 100

---

## Priority 3 — Verify Pre-Order / Customer List Filters

These need a quick code-check to confirm the filters are wired:

### `GET /api/v1/preorders`
Contract filter params: `batch_label`, `status` (pending, invoiced, paid), `page`, `limit`

> Verify that `internal/order/handler.go` (or wherever preorders are handled) reads and applies `batch_label` and `status` filters. If not, wire them.

### `GET /api/v1/customers`
Contract filter params: `page`, `limit`, `search` (by name or email)

> Verify that `internal/customer/handler.go` reads and applies `search` filter via ILIKE on `first_name`, `last_name`, or `email`. If not, wire it.

---

## Migration Note

If `expected_ship_date` column doesn't already exist on `product_variants`, it should be added. Coordinate with Phase 9 migration `013_add_shopify_inventory_item_id.sql` — these can be combined into one migration file to avoid ordering conflicts.

---

## Verification

```bash
# Build check after changes
go build ./...

# Test product list filters
curl "http://localhost:3000/api/v1/products?fulfillment_type=pre_order" | jq .
curl "http://localhost:3000/api/v1/products?sort=price_asc&limit=5" | jq .
curl "http://localhost:3000/api/v1/products?search=rain" | jq .

# Test field names on patch endpoints
curl -X PATCH http://localhost:3000/api/v1/products/1/status \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"fulfillment_type": "pre_order", "preorder_batch_label": "August 2026", "expected_ship_date": "2026-08-01"}'

curl -X PATCH http://localhost:3000/api/v1/products/1/batch \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"preorder_batch_label": "September 2026", "expected_ship_date": "2026-09-01"}'

# Test variants wrapper shape
curl "http://localhost:3000/api/v1/products/1/variants" | jq '.data | keys'
# Expected: ["product_id", "variants"]
```
