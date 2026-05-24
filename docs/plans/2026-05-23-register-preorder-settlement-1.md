# Phase 4.5 Plan: Register Endpoint (Dev-Only) + Phase 5: Pre-order & Settlement

> **Status:** Phase 4.5 = ✅ COMPLETE (small change) | Phase 5 = ❌ Not started  
> **Note on Register:** Temporary endpoint for FE dev to create users without direct DB access. Should be removed or gated before production.

---

## Part A — Register Endpoint (Dev-Only)

### Context

The `POST /auth/register` endpoint was intentionally removed earlier because the real contract (`api_contract_refined.md`) doesn't include self-service registration — users are expected to be created via Shopify customer flow in production.

However, during development: **neither FE nor BE have direct DB access**, making it impossible to create test users. Adding register back temporarily unblocks FE development.

### What already exists (nothing to create)

| File | Status |
|------|--------|
| `AuthStore.CreateUser()` | ✅ Already implemented in `store.go` + `postgres.go` |
| `bcrypt` hashing | ✅ Already in `service.go` as a dependency |
| `validator` | ✅ Already used in handler |

### Changes Required

#### [MODIFY] `internal/auth/dto.go`
Add `RegisterRequest`:
```go
// DEV ONLY — remove before production
type RegisterRequest struct {
    Email    string `json:"email" validate:"required,email"`
    Password string `json:"password" validate:"required,min=8"`
}
```

#### [MODIFY] `internal/auth/service.go`
Add `Register` to `AuthService` interface + implement:
```go
Register(ctx context.Context, req RegisterRequest) (*TokenResponse, error)
```
Logic:
1. Check if email already exists → 409 conflict
2. Hash password with bcrypt (cost 12)
3. `store.CreateUser()` → save to DB
4. Return same `TokenResponse` as login (access + refresh token)

#### [MODIFY] `internal/auth/handler.go`
Add `Register` handler + wire to route:
```go
// DEV ONLY — remove before production
group.Post("/register", rateLimit, h.Register)
```

### Constraints
- Rate limited same as login (5/15min) — prevents abuse even in dev
- No email verification — dev shortcut, acceptable
- `role` defaults to `customer` — no admin self-registration
- Clearly commented `// DEV ONLY` in both handler and route

---

## Part B — Phase 5: Pre-order & Settlement

### Context

With orders implemented, the pre-order payment lifecycle needs its own module. From `api_contract_refined.md`:

- Pre-orders have a separate **settlement lifecycle**: `pending → invoiced → paid`
- When ALL settlements for an order are `paid`, `orders.financial_status` auto-updates to `paid`
- Admins trigger the invoice (`pending → invoiced`), then mark as paid when customer pays (`invoiced → paid`)

This is **intentionally separated** from the `order/` module — settlement is a distinct domain object with its own state machine.

### New Migration Required

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
CREATE INDEX idx_settlements_status ON preorder_settlements(status);
```

### New Module: `internal/preorder/`

Following the standard feature anatomy from the Technical Constitution:

```
internal/preorder/
├── handler.go           # HTTP handlers
├── service.go           # Pure business logic + state machine
├── store.go             # interface PreorderStore
├── postgres.go          # GORM implementation
├── mock_store.go        # For unit tests
└── dto.go               # Request/Response types
```

#### `store.go` — Interface
```go
type PreorderStore interface {
    GetSettlementByID(ctx context.Context, id string) (*Settlement, error)
    ListByOrderID(ctx context.Context, orderID string) ([]Settlement, error)
    ListSettlements(ctx context.Context, filter SettlementFilter) ([]Settlement, int64, error)
    UpdateSettlementStatus(ctx context.Context, id string, status string, timestamp *time.Time) error
    AllSettlementsPaid(ctx context.Context, orderID string) (bool, error)
}
```

#### `service.go` — State Machine (pure functions)
```
pending → [PATCH /invoice] → invoiced → [PATCH /paid] → paid
```
- `InvoiceSettlement(settlement)` — validates current status is `pending`, transitions to `invoiced`
- `MarkSettlementPaid(settlement)` — validates current status is `invoiced`, transitions to `paid`
- Invalid transition → `409 conflict` with clear message

Also: `CheckAndUpdateOrderStatus(ctx, orderID)` — called after every settlement status change:
- If all settlements for the order are `paid` → update `orders.financial_status = 'paid'`

#### `handler.go` — Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/preorders` | List pre-order items, filter by `batch_label`, `status` |
| GET | `/api/v1/preorders/settlements/:id` | Get single settlement detail |
| PATCH | `/api/v1/preorders/settlements/:id/invoice` | Transition `pending → invoiced` |
| PATCH | `/api/v1/preorders/settlements/:id/paid` | Transition `invoiced → paid` |

All endpoints: `Bearer` auth required, admin role enforced via `rbac` middleware.

#### `dto.go`
```go
type Settlement struct {
    ID         string     `json:"id"`
    OrderID    string     `json:"order_id"`
    Status     string     `json:"status"`
    Amount     string     `json:"amount"`
    InvoicedAt *time.Time `json:"invoiced_at,omitempty"`
    PaidAt     *time.Time `json:"paid_at,omitempty"`
    CreatedAt  time.Time  `json:"created_at"`
}

type SettlementFilter struct {
    BatchLabel string
    Status     string
    Page       int
    Limit      int
}
```

### Routing Wiring

#### [MODIFY] `cmd/server/main.go`
```go
preorderStore := preorder.NewPostgresPreorderStore(db)
preorderService := preorder.NewPreorderService(preorderStore, orderStore)
preorderHandler := preorder.NewPreorderHandler(preorderService)
preorderHandler.SetupRoutes(apiV1, jwtSecret)
```

### Open Questions for Phase 5

1. **Settlement creation timing**: When is a settlement record created?
   - Option A: When the order is created (if it has `pre_order` items) — recommended
   - Option B: When admin manually creates it
2. **One settlement per order, or per item?** The contract shows settlement at order level, not item level. Confirm.
3. **Email notification**: When `PATCH /invoice` is hit, should we trigger an email? (requires email provider decision)

### Verification Plan

1. Create an order with `pre_order` items → verify `preorder_settlements` row created with status `pending`
2. `PATCH /preorders/settlements/:id/invoice` → verify status transitions to `invoiced`, `invoiced_at` is set
3. `PATCH /preorders/settlements/:id/paid` → verify status transitions to `paid`, `paid_at` is set
4. After final settlement paid → verify `orders.financial_status` updates to `paid`
5. Try invalid transition (`pending → paid` skipping `invoiced`) → verify `409 conflict` returned
6. Try transition on already-`paid` settlement → verify `409 conflict` returned
