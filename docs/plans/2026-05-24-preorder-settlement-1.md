# 2026-05-24 — Pre-order Settlement (Phase 5)

> **Skill:** `scaffold/plan` (technical-constitution §Architecture)  
> **Phases covered:** Phase 5 — Pre-order Settlement module  
> **Authoritative contract:** `docs/api_contract_refined.md`  
> **Progress tracker:** `docs/plans/progress-tracker.md`  
> **Status:** 🔄 In Progress — decisions locked, implementation started

---

## Context Recap

**Session recovered from:** `adb813e6-ec3c-43cf-b152-a7370544d7e7`  
**Last completed phase:** 4.6 (Register endpoint re-added as dev-only, committed 2026-05-23)

### What's already working (verified from codebase)
- ✅ Auth: login, refresh, logout, me, register (dev-only)
- ✅ Cart: full CRUD, guest + auth, merge, split (ship_ready / pre_order)
- ✅ Products: sync from Shopify, list, variant price override
- ✅ Orders: create (splits cart → Shopify checkout URL + Draft Order), list, get by ID, accept, cancel, step, received
- ✅ `internal/order/store.go` has: `CreateOrder`, `GetOrder`, `GetOrdersByCustomer`, `UpdateOrderStatus`
- ✅ `migrations/` has 001–004 + 006 (no 005, 007 created in this phase)

### What's missing (Phase 5 scope)
- ❌ `migrations/007_create_preorder_settlements.up.sql`
- ❌ `internal/preorder/` module entirely
- ❌ `order/service.go` doesn't auto-create settlement on order create
- ❌ `cmd/server/main.go` not wired with preorder handler

---

## Decisions (Locked)

> All three open questions resolved by user on 2026-05-24. No further blocking.

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | **Auto-create settlement at `POST /orders`** | If any `pre_order` items in cart, create one `preorder_settlements` row immediately |
| 2 | **One settlement per order** (not per item) | All pre_order items share one Shopify Draft Order → one combined pelunasan invoice. Item-level lifecycle tracked separately in `order_items.item_status` |
| 3 | **No email for now** | Prepare boilerplate hook in `service.go` (`// TODO Phase 8: trigger email`). Wire real email in Phase 8 |

---

## Proposed Changes

### 1. Migration

#### [NEW] `migrations/007_create_preorder_settlements.up.sql`
```sql
CREATE TABLE preorder_settlements (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',  -- pending | invoiced | paid
    amount      NUMERIC(12,2) NOT NULL,
    invoiced_at TIMESTAMPTZ,
    paid_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_settlements_order_id ON preorder_settlements(order_id);
CREATE INDEX idx_settlements_status   ON preorder_settlements(status);
```

#### [NEW] `migrations/007_create_preorder_settlements.down.sql`
```sql
DROP TABLE IF EXISTS preorder_settlements;
```

---

### 2. New Module: `internal/preorder/`

Following the standard 5-file feature anatomy (Constitution §Architecture).

#### [NEW] `internal/preorder/store.go`

Domain models + interface:
```go
type Settlement struct {
    ID         string
    OrderID    string
    Status     string     // pending | invoiced | paid
    Amount     float64
    InvoicedAt *time.Time
    PaidAt     *time.Time
    CreatedAt  time.Time
    UpdatedAt  time.Time
}

type SettlementFilter struct {
    Status string
    Page   int
    Limit  int
}

type PreorderStore interface {
    CreateSettlement(ctx context.Context, s *Settlement) error
    GetSettlementByID(ctx context.Context, id string) (*Settlement, error)
    ListSettlements(ctx context.Context, filter SettlementFilter) ([]Settlement, int64, error)
    UpdateSettlementStatus(ctx context.Context, id string, status string, ts *time.Time) error
    AllSettlementsPaid(ctx context.Context, orderID string) (bool, error)
}
```

#### [NEW] `internal/preorder/postgres.go`

GORM implementation of `PreorderStore`.

Key method — `AllSettlementsPaid`: counts rows where `status != 'paid'` for the order; returns `true` if count is 0.

#### [NEW] `internal/preorder/service.go`

Pure state machine (no I/O directly — receives store via constructor injection):

```
pending → [InvoiceSettlement()] → invoiced → [MarkSettlementPaid()] → paid
```

Methods:
- `InvoiceSettlement(ctx, id string) (*SettlementResponse, error)` — validates `status == "pending"`, sets `invoiced_at = now()`, transitions to `invoiced`
- `MarkSettlementPaid(ctx, id string) (*SettlementResponse, error)` — validates `status == "invoiced"`, sets `paid_at = now()`, transitions to `paid`; then calls `checkAndUpdateOrderStatus`
- `checkAndUpdateOrderStatus(ctx, orderID string) error` — if `AllSettlementsPaid` → update `orders.aggregate_status = "paid"` via `order.Store.UpdateOrderStatus`
- `ListSettlements(ctx, filter) ([]SettlementResponse, int64, error)`
- `GetSettlement(ctx, id string) (*SettlementResponse, error)`

Invalid transitions return `apierror.New(http.StatusConflict, "invalid_transition", "Settlement is already <status>")`.

> **Cross-package dependency:** `preorder.service` imports `order.Store` interface (not implementation). No circular import since `order` does not import `preorder`.

#### [NEW] `internal/preorder/handler.go`

| Method | Route | Auth | Description |
|--------|-------|------|-------------|
| GET | `/api/v1/preorders` | Admin JWT | List settlements — filter by `status`, `page`, `limit` |
| GET | `/api/v1/preorders/settlements/:id` | Admin JWT | Single settlement detail |
| PATCH | `/api/v1/preorders/settlements/:id/invoice` | Admin JWT | `pending → invoiced` |
| PATCH | `/api/v1/preorders/settlements/:id/paid` | Admin JWT | `invoiced → paid` |

All routes: `middleware.Auth(jwtSecret)` + `middleware.RBAC("admin")`.

#### [NEW] `internal/preorder/dto.go`

```go
type SettlementResponse struct {
    ID         string     `json:"id"`
    OrderID    string     `json:"order_id"`
    Status     string     `json:"status"`
    Amount     float64    `json:"amount"`
    InvoicedAt *time.Time `json:"invoiced_at,omitempty"`
    PaidAt     *time.Time `json:"paid_at,omitempty"`
    CreatedAt  time.Time  `json:"created_at"`
}

type ListSettlementsQuery struct {
    Status string `query:"status"`
    Page   int    `query:"page"`
    Limit  int    `query:"limit"`
}
```

---

### 3. Extend Order Service

#### [MODIFY] `internal/order/service.go`

Add `preorderStore PreorderStore` field to the `service` struct.  
Update `NewOrderService` constructor to accept it.  
After `s.store.CreateOrder(ctx, order)` succeeds, if any items have `Type == "pre_order"`:

```go
// Auto-create settlement for pre_order items
if hasPreOrder {
    settlement := &preorder.Settlement{
        OrderID: order.ID,
        Amount:  depositTotal,
        Status:  "pending",
    }
    _ = s.preorderStore.CreateSettlement(ctx, settlement)
}
```

#### [MODIFY] `internal/shared/response/envelope.go`

Add `Meta` struct and `SuccessWithMeta()` function for paginated list responses.

---

### 4. Wire in `cmd/server/main.go`

```go
preorderStore   := preorder.NewPostgresPreorderStore(db)
preorderService := preorder.NewPreorderService(preorderStore, orderStore)
preorderHandler := preorder.NewPreorderHandler(preorderService, cfg.Auth.Secret)
preorderHandler.SetupRoutes(api)
```

Also update `order.NewOrderService(...)` call to pass `preorderStore`.

---

## File Summary

| File | Action | Status |
|------|--------|--------|
| `migrations/007_create_preorder_settlements.up.sql` | [NEW] | ✅ Done |
| `migrations/007_create_preorder_settlements.down.sql` | [NEW] | ✅ Done |
| `internal/preorder/store.go` | [NEW] | ✅ Done |
| `internal/preorder/postgres.go` | [NEW] | ✅ Done |
| `internal/preorder/service.go` | [NEW] | ✅ Done |
| `internal/preorder/handler.go` | [NEW] | ✅ Done |
| `internal/preorder/dto.go` | [NEW] | ✅ Done |
| `internal/shared/response/envelope.go` | [MODIFY] add `Meta` + `SuccessWithMeta` | ✅ Done |
| `internal/order/service.go` | [MODIFY] auto-create settlement + inject preorderStore | ❌ Pending |
| `cmd/server/main.go` | [MODIFY] wire preorder handler | ❌ Pending |

---

## Verification Plan

1. `docker-compose down -v && docker-compose up -d --build` — clean state
2. `POST /api/v1/auth/register` → create test admin user
3. Add `pre_order` items to cart → `POST /api/v1/orders` → verify `preorder_settlements` row exists with `status=pending`
4. `GET /api/v1/preorders/settlements/:id` → returns row
5. `PATCH /preorders/settlements/:id/invoice` → status becomes `invoiced`, `invoiced_at` set
6. `PATCH /preorders/settlements/:id/paid` → status becomes `paid`, `paid_at` set; `orders.aggregate_status` → `paid`
7. Try `pending → paid` (skip invoiced) → `409 conflict`
8. Try `invoiced → invoice` again → `409 conflict`
9. `GET /api/v1/preorders?status=paid` → returns the settlement
