# Phase 6 Plan: Order Module Completion & Customer Module

> **Status:** 🔲 PENDING
> **Scope:** Complete Order contract gaps, implement Customer module
> **Pre-requisite commits:** Phase 4 (orders/products), Phase 5 (preorder settlements)
> **Source:** `docs/api_contract_refined.md` Sections 4 & 7

---

## Current State

| Module | What's Built | What's Missing |
|--------|-------------|----------------|
| `order/` | `POST /orders` (create), `GET /orders` (list) | `GET /:id`, `PATCH /:id/accept`, `PATCH /:id/cancel`, `PATCH /:orderId/items/:itemId/step`, `PATCH /:orderId/items/:itemId/received` |
| `customer/` | Nothing | Entire module |

---

## Summary

Phase 6 closes the remaining Order contract endpoints and introduces the Customer module. These are the last two **functional modules** before reporting and webhooks. No new infrastructure changes needed — this phase only adds service methods, store queries, and handler routes on top of existing patterns.

---

## Gap Analysis

**Order gaps (contract vs implementation):**
- `GET /orders/:id` — `GetOrder` exists in `store.go` but is not exposed in `service.go` or `handler.go`
- `PATCH /orders/:id/accept` — `UpdateOrderStatus` exists in store but no handler/service method
- `PATCH /orders/:id/cancel` — same; cancel also needs to trigger a partial refund note (logged, not implemented until Phase 8)
- `PATCH /:orderId/items/:itemId/step` — `order_items` has `item_status` field but no `fulfillment_step` column; migration needed
- `PATCH /:orderId/items/:itemId/received` — `order_items` has no `items_received` column; migration needed

**Customer gaps (none built yet):**
- New module: `internal/customer/` with `store.go`, `postgres.go`, `service.go`, `handler.go`, `dto.go`
- New migration: `008_create_customer_addresses.up.sql` (addresses table, users table already has customer rows)

---

## Proposed Changes

---

### Part 1: Order Module — Missing Endpoints

#### [NEW] `migrations/008_add_order_item_fulfillment.up.sql`
Add two columns to `order_items`:
- `fulfillment_step INT NOT NULL DEFAULT 1` — tracks step 1–4 per item
- `items_received INT NOT NULL DEFAULT 0` — how many units physically received

#### [MODIFY] `internal/order/store.go`
Add to `Store` interface:
- `GetOrder(ctx, orderID, customerID string) (*Order, error)` — customer-scoped fetch
- `UpdateOrderItemStep(ctx, itemID string, step int) error`
- `UpdateOrderItemReceived(ctx, itemID string, count int) error`

Add to `Order` / `OrderItem` domain structs:
- `OrderItem.FulfillmentStep int`
- `OrderItem.ItemsReceived int`

#### [MODIFY] `internal/order/postgres.go`
Implement the three new store methods. `GetOrder` must join `order_items` and verify `customer_id` matches to prevent unauthorized access.

#### [MODIFY] `internal/order/service.go`
Add to `OrderService` interface and implement:
- `GetOrder(ctx, userID, orderID string) (*OrderResponse, error)` — returns items grouped as `ship_ready[]` and `pre_order[]`
- `AcceptOrder(ctx, userID, orderID, fulfillmentType string) error` — validates transition `pending → on_progress` for specified type
- `CancelOrder(ctx, userID, orderID, fulfillmentType, reason string) error` — validates cancellable statuses; logs refund TODO
- `UpdateFulfillmentStep(ctx, userID, orderID, itemID string, step int) error` — validates step 1–4
- `UpdateItemsReceived(ctx, userID, orderID, itemID string, count int) error`

#### [MODIFY] `internal/order/dto.go`
Add request DTOs: `AcceptOrderRequest`, `CancelOrderRequest`, `UpdateStepRequest`, `UpdateReceivedRequest`.
Update `OrderResponse` to include grouped `line_items.ship_ready[]` and `line_items.pre_order[]` with `fulfillment_step` and `items_received`.

#### [MODIFY] `internal/order/handler.go`
Register all missing routes under the `authGrp`:
- `GET /:id` → `GetOrder`
- `PATCH /:id/accept` → `AcceptOrder`
- `PATCH /:id/cancel` → `CancelOrder`
- `PATCH /:id/items/:itemId/step` → `UpdateFulfillmentStep`
- `PATCH /:id/items/:itemId/received` → `UpdateItemsReceived`

---

### Part 2: Customer Module

#### [NEW] `migrations/009_create_customer_addresses.up.sql`
Create `customer_addresses` table:
- `id`, `customer_id` (FK → users.id), `address1`, `city`, `province`, `country`, `zip`, `is_default bool`, `created_at`, `updated_at`

#### [NEW] `internal/customer/store.go`
Domain models + `CustomerStore` interface:
- `ListCustomers(ctx, filter) ([]Customer, int64, error)` — paginated
- `GetCustomerByID(ctx, id string) (*Customer, error)` — includes addresses
- `GetOrdersByCustomer(ctx, customerID string) ([]CustomerOrder, error)` — lightweight order list

`Customer` struct wraps `auth.User` fields with `Addresses []Address` and `OrdersCount int`.

#### [NEW] `internal/customer/postgres.go`
GORM implementation. `GetCustomerByID` uses GORM Preload for addresses. `ListCustomers` paginates via `LIMIT/OFFSET`.

#### [NEW] `internal/customer/service.go`
- `ListCustomers(ctx, filter) ([]CustomerResponse, int64, error)`
- `GetCustomer(ctx, id string) (*CustomerDetailResponse, error)`
- `GetCustomerOrders(ctx, customerID string) ([]CustomerOrderResponse, error)`

All admin-only (no customer-facing logic — just data retrieval for admin panel).

#### [NEW] `internal/customer/dto.go`
`CustomerResponse`, `CustomerDetailResponse`, `CustomerOrderResponse`, `ListCustomersQuery`.

#### [NEW] `internal/customer/handler.go`
All routes under `/api/v1/customers`, all require `middleware.Auth` + `middleware.RBAC("admin")`:
- `GET /` → `ListCustomers` (query params: `page`, `limit`, `search`)
- `GET /:id` → `GetCustomer`
- `GET /:id/orders` → `GetCustomerOrders`

#### [MODIFY] `cmd/server/main.go`
Initialize and wire `customerStore`, `customerService`, `customerHandler`. Mount `/api/v1/customers`.

---

## Verification Plan

```bash
# Build check
go build ./...

# Unit tests
go test ./internal/order/... -v
go test ./internal/customer/... -v
```

### Manual E2E
```
1. POST /auth/login (admin)             → get admin token
2. POST /auth/login (customer)          → get customer token
3. POST /orders (customer)              → create order, note order_id
4. GET  /orders/:id (customer)          → verify grouped line_items
5. PATCH /orders/:id/accept (admin)     → body: { fulfillment_type: "ship_ready" }
6. PATCH /orders/:id/items/:id/step     → body: { fulfillment_step: 2 }
7. PATCH /orders/:id/items/:id/received → body: { items_received: 1 }
8. PATCH /orders/:id/cancel (admin)     → body: { fulfillment_type: "pre_order", reason: "test" }
9. GET  /customers (admin)             → verify customer list includes test user
10. GET /customers/:id (admin)          → verify addresses array present
11. GET /customers/:id/orders (admin)   → verify order in history
```
