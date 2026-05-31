# API Contract Alignment Plan

> **Status:** 🔲 PENDING
> **Scope:** Standardize API responses and query parameters across all modules to match the `api_contract/` markdown specifications.
> **Pre-requisite:** Database schema alignment (Phase 6 part 0) complete.

---

## Current State vs Contract

| Module | What's Built | What's Expected by Contract |
|--------|-------------|-----------------------------|
| `product/` | Flat array of variants in `data`, no pagination | `data` contains pagination metadata + nested `products` array. Products contain nested `variants` and `images` arrays. Query supports `page`, `limit`, `sort`, `search`, `fulfillment_type`. |
| `order/` | Flat array of orders in `data`, no pagination | `data` contains pagination metadata + `orders` array. Query supports `page`, `limit`, `status`, `search`. |
| `customer/` | Pagination meta outside `data` root | `data` contains pagination metadata + `customers` array. Query supports `page`, `limit`, `search`. |

---

## Architecture Decisions (from Frontend Feedback)

### Decision 1 — Pagination Level: Product vs Variant

**Background:** The frontend team noted that because 1 product can have multiple variants, paginating by product means the number of variant rows rendered on the frontend can exceed the `limit` parameter.

**Decision: Paginate by Product (backend stays as-is).**

Rationale:
- The `api_contract/Get Product List` contract already returns a top-level `products` array, not a `variants` array. The intended design is product-level pagination.
- Variant-level pagination on the backend would require fragmenting products across pages, producing broken UX (e.g., half a product on page 1, the rest on page 2).
- The frontend should render **1 row = 1 Product** with expandable/collapsible variant rows beneath it (consistent with how Shopify Admin itself works).
- The pagination count (`limit: 10`) refers to 10 Products, not 10 variants.

### Decision 2 — `inventory_quantity` Source of Truth

**Background:** The frontend team needs `inventory_quantity` per variant to trigger stock-depletion modals and block add-to-cart when stock is 0. The current schema and API response do not include this field.

**Decision: Store `inventory_quantity` locally in `product_variants`, synced from Shopify during `POST /products/sync`.**

Rationale:
- Fetching live from Shopify per-request adds significant latency and API rate-limit risk at scale.
- The PRD already accepts that stock is validated at two hard points (add-to-cart and start checkout) — a slightly stale display value is acceptable for the product listing page.
- Shopify's Admin GraphQL variant node exposes `inventoryQuantity` natively; we already query variants during sync, so it's a zero-overhead addition.

---

## Proposed Changes (Helicopter View)

### 0. Migration: Add `inventory_quantity` to `product_variants`
- New migration (`012_add_inventory_quantity.up.sql`): `ALTER TABLE product_variants ADD COLUMN inventory_quantity INT NOT NULL DEFAULT 0`.

### 1. Shared Pagination Utility
- Implement a shared DTO structure for paginated responses (e.g., `PaginatedResponse`) containing `page`, `limit`, `total`, `totalPages`, and a generic data payload. This ensures all list endpoints follow the exact same envelope structure as defined in the contract.

### 2. Product Module Overhaul
- **DTOs:** Create `ProductQuery` struct for filtering. Update `ProductDTO` to nest `variants` (each variant includes `inventory_quantity`) and `images` according to the `Get Product List` contract.
- **Store:** Modify `GetVariants` to become a paginated `GetProducts` method. Use GORM's `Preload` to eagerly load variants alongside the products. Apply SQL filtering for search and status.
- **Sync:** Update `SyncFromShopify` in `service.go` to query `inventoryQuantity` from the Shopify GraphQL variant node and store it during upsert.
- **Service & Handler:** Parse query parameters, invoke the paginated store method, and wrap the resulting nested DTOs in the shared pagination envelope.

### 3. Order Module Alignment
- **DTOs:** Add `OrderQuery` struct. Update `OrderListResponse` to match the expected pagination structure.
- **Store:** Update the `GetOrders` method to accept pagination limits and filters (`status`, `search`).
- **Service & Handler:** Map the database results into the standard pagination envelope, ensuring the array is named `orders`.

### 4. Customer Module Alignment
- **Handler:** The current implementation places the `meta` object at the root of the response. Refactor the response serialization to place `page`, `limit`, and `total` inside the `data` object, matching the exact shape of the `Get Customer List` contract.

---

## Verification Plan

Once implemented, the following should be verified:
1. **Product List:** `GET /api/v1/products?page=1&limit=5&fulfillment_type=ship_ready` returns a `data` object with `totalPages` and a `products` array where each product contains nested `variants` with `inventory_quantity`.
2. **Product Sync:** After `POST /products/sync`, check `product_variants` table to confirm `inventory_quantity` is populated from Shopify.
3. **Order List:** `GET /api/v1/orders` returns a `data` object with `orders` array and pagination metadata.
4. **Customer List:** `GET /api/v1/customers` returns a `data` object with `customers` array and pagination metadata.
