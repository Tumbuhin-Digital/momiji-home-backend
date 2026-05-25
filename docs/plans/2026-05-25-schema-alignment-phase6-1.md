# Phase 6 Plan (Revised): Schema Alignment + Order Completion + Customer Module

> **Status:** 🔲 PENDING
> **Scope:** Align DB schema with `docs/dbdiagram.txt`, complete Order endpoints, finalize Customer module
> **Pre-requisite:** Phase 5 complete ✅
> **Replaces:** `2026-05-25-order-completion-customer-1.md`
> **Decision:** Align to DB diagram — separate `customers` table, per-line-item settlements

---

## Decision Log

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | **Separate `customers` table** (not extend `users`) | Matches DB diagram; `users` = auth identity, `customers` = business profile |
| 2 | **Per-line-item settlement** (`order_line_item_id` FK) | Matches DB diagram; allows granular balance tracking per pre_order item |
| 3 | **Fix `orders` schema** to match DB diagram columns | Required for contract-compliant responses |
| 4 | **Fix `order_line_items`** (rename from `order_items`) | DB diagram uses `order_line_items`; add missing columns |

---

## Part 0 — Schema Corrections (Prerequisite Migrations)

These must run before any Go code changes.

### [NEW] `migrations/008_fix_orders_schema.up.sql`
`ALTER TABLE orders ADD COLUMN`:
- `order_number VARCHAR(16) UNIQUE` — app generates `ORD-{short-id}` at creation time
- `financial_status VARCHAR(20) NOT NULL DEFAULT 'pending'` — replaces `aggregate_status` semantically
- `fulfillment_status VARCHAR(20) NOT NULL DEFAULT 'pending'`
- `shipping_address_id UUID REFERENCES customer_addresses(id)` — needs Part 2's table first; add as nullable
- `shipping_method VARCHAR(32)`
- `shipping_cost NUMERIC(12,2) NOT NULL DEFAULT 0`
- `total_ship_ready NUMERIC(12,2) NOT NULL DEFAULT 0`
- `total_deposit_paid NUMERIC(12,2) NOT NULL DEFAULT 0`
- `total_balance_due NUMERIC(12,2) NOT NULL DEFAULT 0`
- `total_charged_now NUMERIC(12,2) NOT NULL DEFAULT 0`
- `currency VARCHAR(3) NOT NULL DEFAULT 'USD'`
- `note TEXT`

Keep `aggregate_status` and `total_price` temporarily for backward compatibility; deprecate in Phase 7.

### [NEW] `migrations/009_fix_order_items_schema.up.sql`
`ALTER TABLE order_items ADD COLUMN`:
- `title VARCHAR(255)` — product title snapshot at order time
- `unit_price NUMERIC(12,2)` — ws_price snapshot at order time
- `amount_charged NUMERIC(12,2)` — what customer actually paid for this item
- `balance_due NUMERIC(12,2)` — remaining balance (pre_order only)
- `fulfillment_step INT NOT NULL DEFAULT 1` — 1–4 per PRD state machine
- `items_received INT NOT NULL DEFAULT 0`

### [NEW] `migrations/010_create_customers_and_addresses.up.sql`
Create new `customers` table (separate from `users`):
```
customers { id UUID pk, email varchar unique, first_name varchar, last_name varchar,
            phone varchar, shopify_customer_id varchar, created_at, updated_at }
```
Create `customer_addresses` table:
```
customer_addresses { id UUID pk, customer_id UUID → customers.id,
                     first_name, last_name, address1, address2, city, province,
                     country, zip, phone, is_default bool default false, created_at, updated_at }
```

### [NEW] `migrations/011_fix_preorder_settlements_fk.up.sql`
Change settlement FK from per-order to per-line-item:
- `DROP TABLE preorder_settlements` (safe — Phase 5 not in production yet)
- Recreate with schema matching DB diagram:
```
preorder_settlements { id UUID pk, order_line_item_id UUID → order_items.id,
                       balance_amount NUMERIC(12,2), status VARCHAR(20) DEFAULT 'pending',
                       due_date DATE, invoiced_at TIMESTAMPTZ null, paid_at TIMESTAMPTZ null }
```

---

## Part 1 — Order Module: Schema + Code Alignment

### [MODIFY] `internal/order/store.go`
Update `Order` struct to include all new fields (`OrderNumber`, `FinancialStatus`, `FulfillmentStatus`, `ShippingMethod`, `ShippingCost`, `TotalShipReady`, `TotalDepositPaid`, `TotalBalanceDue`, `TotalChargedNow`, `Currency`, `Note`).

Update `OrderItem` struct: add `Title`, `UnitPrice`, `AmountCharged`, `BalanceDue`, `FulfillmentStep`, `ItemsReceived`.

Add to `Store` interface:
- `GetOrder(ctx, orderID, customerID string) (*Order, error)` — scoped by customer
- `UpdateOrderItemStep(ctx, itemID string, step int) error`
- `UpdateOrderItemReceived(ctx, itemID string, count int) error`
- `UpdateOrderStatus(ctx, orderID, financialStatus, fulfillmentStatus string) error` — replaces current single-status method

### [MODIFY] `internal/order/postgres.go`
Implement all new store methods. `GetOrder` must `Preload("Items")` and verify `customer_id`.

### [MODIFY] `internal/order/service.go`
- Update `CreateOrder` to populate all new `Order` fields at creation time (compute `total_ship_ready`, `total_deposit_paid`, `total_balance_due`, `total_charged_now`, set `order_number`)
- Add to `OrderService` interface and implement:
  - `GetOrder(ctx, userID, orderID string) (*OrderDetailResponse, error)`
  - `AcceptOrder(ctx, userID, orderID, fulfillmentType string) error`
  - `CancelOrder(ctx, userID, orderID, fulfillmentType, reason string) error`
  - `UpdateFulfillmentStep(ctx, userID, orderID, itemID string, step int) error`
  - `UpdateItemsReceived(ctx, userID, orderID, itemID string, count int) error`

### [MODIFY] `internal/order/dto.go`
- Update `OrderResponse` → `OrderDetailResponse` with grouped `line_items.ship_ready[]` + `line_items.pre_order[]`, each including `fulfillment_step` and `items_received`
- Add request DTOs: `AcceptOrderRequest`, `CancelOrderRequest`, `UpdateStepRequest`, `UpdateReceivedRequest`

### [MODIFY] `internal/order/handler.go`
Register all missing routes on the authenticated group:
- `GET /:id` → `GetOrder`
- `PATCH /:id/accept` → `AcceptOrder`
- `PATCH /:id/cancel` → `CancelOrder`
- `PATCH /:id/items/:itemId/step` → `UpdateFulfillmentStep`
- `PATCH /:id/items/:itemId/received` → `UpdateItemsReceived`

---

## Part 2 — Customer Module: Rebuild on Correct Schema

The customer module was scaffolded against `users` table via `TableName() → "users"`. It must be rebuilt against the new `customers` table.

### [MODIFY] `internal/customer/store.go`
Change `Customer` struct to map to new `customers` table (remove `TableName() → "users"` override). Add full fields: `FirstName`, `LastName`, `Phone`, `ShopifyCustomerID`.

Change `Address` struct to map to new `customer_addresses` table (add `FirstName`, `LastName`, `Address2`, `Phone`).

`CustomerStore` interface unchanged — same three methods.

### [MODIFY] `internal/customer/postgres.go`
Rewrite queries to read from `customers` table (not `users`). `GetCustomerByID` uses GORM `Preload("Addresses")`. `ListCustomers` accepts `search` on `email`, `first_name`, `last_name`.

### [MODIFY] `internal/customer/service.go`
No interface changes — same three service methods. Internal DTO mapping updated to include `first_name`, `last_name`, `phone`, `address2` fields now available.

### [MODIFY] `internal/customer/dto.go`
Update `CustomerResponse` and `CustomerDetailResponse` to include `first_name`, `last_name`, `phone`. Update `AddressResponse` to include `first_name`, `last_name`, `address2`, `phone`.

### [MODIFY] `internal/customer/handler.go`
`ListCustomers` handler: fix response to use `response.SuccessWithMeta` instead of raw `fiber.Map` (currently not using standard envelope).

### [MODIFY] `cmd/server/main.go`
Wire customer module: `customerStore`, `customerService`, `customerHandler`. Mount `/api/v1/customers`.

---

## Part 3 — Preorder Settlement: Align FK to Line Item

### [MODIFY] `internal/preorder/store.go`
Change `Settlement` struct: replace `OrderID string` with `OrderLineItemID string`. Keep `OrderID` as a derived field (via join) for the cascade check.

Update `PreorderStore` interface:
- `CreateSettlement(ctx, s *Settlement) error` — now takes `order_line_item_id`
- `AllSettlementsPaid(ctx, orderID string) (bool, error)` — joins `order_items` to check by `order_id`

### [MODIFY] `internal/preorder/postgres.go`
Rewrite all queries to use new FK. `AllSettlementsPaid` joins `order_items` on `order_line_item_id` to group by `order_id`.

### [MODIFY] `internal/order/service.go`
Update `CreateOrder`: when creating settlements for pre_order items, pass `OrderLineItemID = item.ID` instead of `OrderID = order.ID`. Create one `Settlement` per pre_order line item (not one per order).

### [MODIFY] `internal/preorder/dto.go`
Update `SettlementResponse`: replace `OrderID` with `OrderLineItemID`. Add `OrderID` as derived field if needed for display.

---

## Verification Plan

```bash
# Schema
make migrate-down
make migrate-up

# Build
go build ./...

# Unit tests
go test ./internal/order/... -v
go test ./internal/customer/... -v
go test ./internal/preorder/... -v
```

### Manual E2E
```
1. POST /auth/login (admin)               → admin_token
2. POST /auth/login (customer)            → customer_token
3. POST /orders (customer w/ pre_order)   → check response has order_number, financial_status, total_ship_ready etc.
4. SELECT * FROM preorder_settlements;    → verify order_line_item_id populated (not order_id)
5. GET  /orders/:id                       → verify line_items.ship_ready + line_items.pre_order grouping
6. PATCH /orders/:id/accept               → { fulfillment_type: "ship_ready" }
7. PATCH /orders/:id/items/:id/step       → { fulfillment_step: 2 }
8. PATCH /orders/:id/items/:id/received   → { items_received: 1 }
9. GET  /customers (admin)                → verify first_name, last_name, phone present
10. GET /customers/:id                    → verify addresses array with address2, phone
11. GET /customers/:id/orders             → verify order list
```
