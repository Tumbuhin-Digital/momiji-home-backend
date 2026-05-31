# Phase 7: API Contract Alignment — Implementation

> **Status:** 🔲 PENDING
> **Date:** 2026-05-31
> **Pre-requisite:** Migrations 008–012 applied ✅, Order handler all 6 routes wired ✅, Product DTOs updated ✅

---

## Current State Snapshot

| Module | Schema | DTOs | Store | Service | Handler | Status |
|--------|--------|------|-------|---------|---------|--------|
| `product/` | ✅ `inventory_quantity` in DB | ✅ `ProductDTO`, `ProductQuery`, `VariantDTO` w/ `inventory_quantity` | 🔲 `GetProducts` with pagination/filter | 🔲 wiring | 🔲 query parsing | Partially done |
| `order/` | ✅ | ✅ `OrderQuery`, `PaginatedData` | 🔲 filter params | 🔲 filter in service | ✅ handler wired | Partially done |
| `preorder/` | ✅ FK to `order_line_items` | 🔲 Response shape doesn't match contract | 🔲 `batch_label` filter, join to order/item | 🔲 richer DTO | ✅ routes exist | Partially done |
| `customer/` | ✅ | ✅ | 🔲 | 🔲 | 🔲 not wired in `main.go` | Not wired |

---

## Part 1 — Product Module: Complete the wiring

### What's missing
The product `dto.go` and `store.go` types are updated, but `service.go` and `handler.go` still call the old `GetVariants()` — which returns a flat list without pagination or filtering.

### Changes needed

**`internal/product/store.go`**
- Add `GetProducts(ctx, query ProductQuery) ([]Product, int64, error)` to the `Store` interface
- Each `Product` should preload its `Variants` (and images if stored)

**`internal/product/postgres.go`**
- Implement `GetProducts`: paginated query using `LIMIT/OFFSET`, filter by `status`/`fulfillment_type` (join to `product_variants`), search by `title`

**`internal/product/service.go`**
- Replace `GetVariants(ctx)` with `GetProducts(ctx, query ProductQuery) ([]ProductDTO, int64, error)`
- Map DB model to `ProductDTO` with nested `VariantDTO` (including `inventory_quantity` and `sku`)
- Update `SyncFromShopify` to query `inventoryQuantity` from Shopify GraphQL and store it on upsert

**`internal/product/handler.go`**
- Update `GetProducts` to parse `ProductQuery` from request query string
- Compute `totalPages` and wrap response in `PaginatedData` using the shared utility (same pattern as order handler)
- Update `GetProductByID` to return the full `ProductDTO` with nested `variants` and `images` (not the old lean `ProductDetailDTO`)

---

## Part 2 — Order Module: Complete store-level filtering

### What's missing
`GetOrders` service/handler accept `OrderQuery` but the store query ignores `status` and `search` filters.

### Changes needed

**`internal/order/store.go`**
- Add `status` and `search` params to the `GetOrders` store interface

**`internal/order/postgres.go`**
- Apply `WHERE fulfillment_status = ?` when `status` is set
- Apply `WHERE order_number ILIKE ? OR customer email ILIKE ?` when `search` is set

**`internal/order/service.go`**
- Pass `query.Status` and `query.Search` from the handler down to the store call

---

## Part 3 — Preorder Module: Align list response to contract

### What the contract expects (`GET /api/v1/preorders`)
The contract response (`Get Pre-Order List`) expects a `preorders` array where each entry is a **flat row** joining settlement data with order + line item info:
```json
{
  "order_id": 191,
  "order_number": "ORD-191",
  "customer_email": "...",
  "item_id": 2,
  "title": "After the Rain Shelf",
  "quantity": 1,
  "balance_due": "90.00",
  "batch_label": "August 2026",
  "settlement_status": "pending",
  "due_date": "2026-08-01"
}
```

### What's currently returned
The current `ListSettlements` returns a raw `Settlement` struct with only `id`, `status`, `balance_amount`, `invoiced_at`, `paid_at`.

### Changes needed

**`internal/preorder/store.go`**
- Add `batch_label` to `SettlementFilter`
- Extend `ListSettlements` to JOIN `order_items`, `orders`, `users` so the query returns the richer row needed by the DTO

**`internal/preorder/dto.go`**
- Add `PreorderListItemResponse` struct matching the contract fields exactly
- Update `ListSettlements` service method return type to `[]PreorderListItemResponse`

**`internal/preorder/handler.go`**
- Update `ListSettlements` to wrap response in `PaginatedData` with `"preorders"` key (same pattern as orders/products)
- Add `batch_label` to query parsing

---

## Part 4 — Customer Module: Wire into main.go

### What's missing
The `internal/customer/` module is fully scaffolded (store, service, handler, DTOs) but is **not mounted** in `cmd/server/main.go`.

### Changes needed

**`cmd/server/main.go`**
- Initialize `customerStore`, `customerService`, `customerHandler`
- Mount on `/api/v1`
- Ensure `customerStore` reads from the new `customers` table (not `users`)

**`internal/customer/handler.go`**
- Wrap `ListCustomers` response in `PaginatedData` with `"customers"` key (currently uses a raw `fiber.Map`)

---

## Verification Plan

```bash
go build ./...
go test ./internal/product/... -v
go test ./internal/order/... -v
go test ./internal/customer/... -v
go test ./internal/preorder/... -v
docker compose up -d --build
```

### Manual checks
```
GET /api/v1/products?page=1&limit=5&fulfillment_type=ship_ready
  → data.products[0].variants[0].inventory_quantity exists

GET /api/v1/products?page=1&limit=5&search=rain
  → filters by title correctly

GET /api/v1/products/:id
  → returns full nested DTO with variants and images

POST /products/sync  (admin)
  → then check DB: SELECT inventory_quantity FROM product_variants LIMIT 5;

GET /api/v1/orders?status=on_progress&search=ORD
  → filters applied correctly

GET /api/v1/preorders?batch_label=August 2026&status=pending
  → returns flat preorder rows with order_number, customer_email, title, batch_label

GET /api/v1/customers?page=1&search=james
  → returns paginated customers from customers table (not users)
```
