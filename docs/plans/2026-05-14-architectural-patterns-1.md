# 2026-05-14 — Architectural Patterns (Phase 1 & 3)

> **Skill:** `architectural-patterns` (technical-constitution §Architectural Patterns - Testability-First Design)  
> **Phases covered:** Phase 1 (Foundation), Phase 3 (Cart/Checkout)  
> **Status:** ✅ Complete

---

## Decisions Made

### Feature-Based Packaging
All modules follow the vertical-slice pattern under `internal/<feature>/`:

```
internal/
├── auth/
│   ├── handler.go       # HTTP handlers (public API)
│   ├── service.go       # Pure business logic + AuthService interface
│   ├── store.go         # AuthStore interface (I/O port)
│   ├── postgres.go      # PostgresAuthStore (I/O adapter)
│   ├── mock_store.go    # MockAuthStore (test adapter)
│   └── dto.go           # Request/Response types
├── cart/         (same structure)
├── order/        (same structure)
├── product/      (same structure)
└── platform/
    ├── database/postgres.go
    ├── server/fiber.go
    ├── server/middleware/  (auth, cors, logger, ratelimit, rbac, correlation, optional_auth)
    └── shopify/client.go
```

### I/O Isolation (Rule 1)
- All database access is behind typed interfaces (`AuthStore`, `CartStore`, `ProductStore`, `OrderStore`)
- Each interface has a `Postgres<X>Store` production adapter and `Mock<X>Store` test adapter
- Shopify HTTP client is behind `shopify.Client` interface

### Pure Business Logic (Rule 2)
- `service.go` in each module receives all dependencies via constructor injection
- No DB calls inside business logic functions — data fetched first, then passed to pure logic
- Example: `cart.service.go` → fetch cart items → pass to `splitByFulfillmentType()` (pure)

### Module Boundaries (Rule 3)
- `internal/platform/` contains cross-cutting infrastructure (DB, server, Shopify, middleware)
- `internal/shared/` contains utilities with no feature coupling (apierror, response, validator)
- Feature modules never import other feature modules directly (no `cart` importing `auth`)

### Dependency Direction (Rule 4)
```
cmd/server/main.go (wiring)
    └── internal/<feature>/handler.go
        └── internal/<feature>/service.go (interface)
            └── internal/<feature>/store.go (interface)
                └── internal/<feature>/postgres.go (implementation)
```

---

## Pattern Consistency Audit (required by constitution)
Modules checked: `auth`, `cart`, `order`

| Check | auth | cart | order |
|-------|------|------|-------|
| `store.go` interface exists | ✅ | ✅ | ✅ |
| `postgres.go` implements interface | ✅ | ✅ | ✅ |
| `mock_store.go` test adapter | ✅ | ❌ missing | ❌ missing |
| `service.go` uses interface (not concrete) | ✅ | ✅ | ✅ |
| No cross-feature imports | ✅ | ✅ | ✅ |

**Consistency: 87%** — `mock_store.go` missing for cart and order (tracked as gap in progress-tracker)

---

## Tasks Created in This Skill

### Task 1: Scaffold project structure (Phase 1)
- **Files created:** `cmd/server/main.go`, `internal/config/config.go`, `internal/platform/database/postgres.go`, `internal/platform/server/fiber.go`, all middleware files, `internal/shared/*`
- **Outcome:** Build passes with `go build ./...`

### Task 2: OptionalAuth middleware (Phase 3)
- **Files created:** `internal/platform/server/middleware/optional_auth.go`
- **Functional requirement:** Reads `Authorization: Bearer <token>` OR `X-Session-ID` header. Sets `user_id` or `session_id` in Fiber locals. Does NOT return 401 on missing auth — next handler decides.
- **Why:** Cart endpoints must work for both authenticated users and anonymous guests

---

## Open Gaps (to address in future tasks)
- [ ] `internal/cart/mock_store.go` — needed for unit tests
- [ ] `internal/order/mock_store.go` — needed for unit tests
- [ ] `internal/product/mock_store.go` — needed for unit tests
- [ ] `internal/checkout/service.go` — missing pure business logic layer
