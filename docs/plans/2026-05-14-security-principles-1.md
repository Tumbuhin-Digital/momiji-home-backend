# 2026-05-14 — Security Principles (Phase 2 & 2.5)

> **Skill:** `security-principles` (technical-constitution §Security Principles)  
> **Phases covered:** Phase 2 (Auth module), Phase 2.5 (API contract refinement)  
> **Status:** ✅ Complete

---

## Decisions Made

### Authentication (OWASP A07: Identification & Authentication Failures)

| Requirement | Implementation | Status |
|-------------|----------------|--------|
| Bcrypt ONLY for passwords | `bcrypt.GenerateFromPassword([]byte(req.Password), 12)` | ✅ |
| Rate limit on auth endpoints | `middleware.RateLimit(5, 15*time.Minute)` on `/login` and `/register` | ✅ |
| JWT access token (short TTL) | 15 minutes, signed with `JWT_SECRET` (HS256) | ✅ |
| JWT refresh token (longer TTL) | 7 days, signed with `JWT_REFRESH_SECRET` | ✅ |
| Token in response body (not cookie) | Per `api_contract_refined.md` — mobile-compatible | ✅ |
| No sensitive data in JWT payload | Claims: `sub` (userID), `role`, `exp`, `iat` only | ✅ |
| Deny by default | `middleware.Auth()` applied to all protected routes; `middleware.RBAC()` for role checks | ✅ |

### Auth Flow Finalized (Phase 2.5 — after refined contract)
```
POST /auth/login      → 200 { access_token, refresh_token, expires_in, user }
POST /auth/refresh    → 200 { access_token, refresh_token, expires_in, user }
POST /auth/logout     → 200 (client-side token discard)
GET  /auth/me         → 200 { user }  [requires Bearer token]
POST /auth/register   → 201 [DEV ONLY — remove before production]
```

### Secrets Management
- `.env` file for all secrets — never committed (gitignored)
- `.env.template` committed with empty values as contract
- Required secrets:
  ```
  JWT_SECRET=           # Access token signing key
  JWT_REFRESH_SECRET=   # Refresh token signing key
  SHOPIFY_ADMIN_API_TOKEN=
  SHOPIFY_STOREFRONT_TOKEN=
  ```
- `config.go` fails fast on missing secrets at startup

### RBAC
- `role` claim in JWT (`customer` | `admin`)
- `middleware.RBAC("admin")` applied to admin-only endpoints (product sync, order admin actions)
- Default role on registration: `customer`

### Input Validation
- All request DTOs validated via `validator.ValidateStruct()` using `go-playground/validator`
- Validation errors returned as `map[string]string` in `error.details` field
- No raw SQL — all queries via GORM parameterized queries

---

## Phase 2.5 — Contract-Driven Security Changes

When `api_contract_refined.md` was introduced, the following security-relevant changes were made:

| What changed | Why |
|--------------|-----|
| `POST /auth/register` removed | Not in contract — users managed via Shopify in production |
| `GET /users/me` → `GET /auth/me` | Consistent auth namespace |
| HttpOnly cookie removed for refresh token | Contract uses body-based tokens for mobile compatibility |
| `user/` package deleted | Merged into `auth/` — eliminates duplicate user-fetching code |

---

## DEV-ONLY Endpoint

`POST /auth/register` was re-added in Phase 4.6 (2026-05-23) as a temporary dev endpoint:
- Marked `// DEV ONLY` in handler, route, and service interface
- Rate limited (5/15min)
- `role` hardcoded to `customer` — no admin self-registration possible
- Must be removed or gated before production deployment

---

## Tasks Created in This Skill

### Task 1: Auth module (Phase 2)
- **Files:** `internal/auth/handler.go`, `service.go`, `store.go`, `postgres.go`, `mock_store.go`, `dto.go`
- **Acceptance criteria:** `POST /auth/login` with valid credentials returns 200 with token pair; invalid credentials return 401; rate limiter blocks after 5 attempts in 15 min

### Task 2: Middleware chain (Phase 1)
- **Files:** `internal/platform/server/middleware/auth.go`, `rbac.go`, `ratelimit.go`
- **Acceptance criteria:** `Auth()` returns 401 on missing/invalid/expired JWT; `RBAC("admin")` returns 403 for non-admin role; `RateLimit(5, 15m)` returns 429 after 5th request
