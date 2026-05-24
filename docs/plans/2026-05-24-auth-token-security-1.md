# 2026-05-24 — Auth Token Security Strategy

> **Skill:** `scaffold/plan` (technical-constitution §Security)  
> **Scope:** Secure token delivery for `POST /auth/login`, `POST /auth/register`, `POST /auth/refresh`, `POST /auth/logout`  
> **Authoritative contract:** `docs/api_contract_refined.md`  
> **Status:** 📋 Plan ready — pending implementation in next session  
> **Decision:** Pattern 1 — Full HttpOnly Cookie Auth (updated 2026-05-24)

---

## Problem Statement

The current `TokenResponse` returns **both** `access_token` and `refresh_token` in the JSON body:

```json
{
  "data": {
    "access_token": "eyJ...",
    "refresh_token": "eyJ...",
    "expires_in": 900,
    "user": { "id": "...", "email": "...", "role": "..." }
  }
}
```

**Risk:** `refresh_token` has a 7-day TTL. Any XSS vulnerability can steal it from JS-accessible storage. Server-side logout is impossible — the current `Logout()` handler literally says so in a comment.

---

## Context: Why Pattern 1 is correct here

| Factor | Value |
|--------|-------|
| Only users who can log in | Admins only (same project — storefront + admin panel) |
| Client type | Browser-based SPA |
| DB bloat from cookies? | **None** — JWTs are stateless; cookies live in the browser, not the DB |
| Middleware refactor effort | Small — read cookie first, fallback to `Authorization` header |

Since only admins log in and the client is always a browser, **full HttpOnly cookie auth** is the cleanest approach. No JS ever touches either token. XSS cannot steal anything.

---

## Decision: Pattern 1 — Full HttpOnly Cookie Auth

**Both tokens → HttpOnly cookies. Zero tokens in response body.**

```
Login response body:          Set-Cookie headers:
{                             access_token=eyJ...; HttpOnly; Secure; SameSite=Strict; Path=/api/v1; Max-Age=900
  "expires_in": 900,          refresh_token=eyJ...; HttpOnly; Secure; SameSite=Strict; Path=/api/v1/auth/refresh; Max-Age=604800
  "user": { ... }
}
```

### Why no DB bloat
JWTs are **stateless** — the server validates them by checking the cryptographic signature with the JWT secret. No session table, no token table, no DB lookup on every request. The cookie is just a transport mechanism; the token itself is self-contained.

### Cookie configuration
```
access_token cookie:
  HttpOnly    ← JS cannot read (XSS-proof)
  Secure      ← HTTPS only (false in development)
  SameSite=Strict  ← CSRF protection
  Path=/api/v1     ← sent on all API calls
  Max-Age=900      ← 15 minutes

refresh_token cookie:
  HttpOnly    ← JS cannot read (XSS-proof)
  Secure      ← HTTPS only (false in development)
  SameSite=Strict  ← CSRF protection
  Path=/api/v1/auth/refresh  ← ONLY sent to refresh endpoint
  Max-Age=604800   ← 7 days
```

Scoping `refresh_token` to `Path=/api/v1/auth/refresh` means the browser **never sends** the long-lived token to any other endpoint — even if the access token path is compromised.

---

## New Response Shape

### `POST /auth/login` and `POST /auth/register`
```json
// Response body (no tokens)
{
  "status": "success",
  "message": "Login successful",
  "data": {
    "expires_in": 900,
    "user": {
      "id": "uuid",
      "email": "admin@momijihome.com",
      "role": "admin",
      "shopify_customer_id": null
    }
  },
  "timestamp": "2026-05-24T..."
}
// Set-Cookie: access_token=eyJ...; HttpOnly; ...
// Set-Cookie: refresh_token=eyJ...; HttpOnly; ...; Path=/api/v1/auth/refresh
```

### `POST /auth/refresh`
```json
// Request: no body, no Authorization header — browser sends refresh_token cookie automatically
// Response body:
{
  "status": "success",
  "message": "Token refreshed successfully",
  "data": { "expires_in": 900 }
}
// Set-Cookie: access_token=eyJ...; (rotated)
// Set-Cookie: refresh_token=eyJ...; (rotated)
```

### `POST /auth/logout`
```json
// Response body:
{ "status": "success", "message": "Logged out successfully" }
// Set-Cookie: access_token=; Max-Age=-1  (cleared)
// Set-Cookie: refresh_token=; Max-Age=-1  (cleared)
```

---

## What Needs to Change

### Files to modify

| File | Change |
|------|--------|
| `internal/auth/dto.go` | Remove `RefreshToken` from `TokenResponse`; remove `RefreshRequest` struct |
| `internal/auth/handler.go` | Set both cookies on login/register/refresh; clear both on logout; read refresh from cookie; add `setTokenCookies()` + `clearTokenCookies()` helpers |
| `internal/auth/service.go` | Add internal `tokenPair` struct (or use `json:"-"` tag) to carry refresh token to handler without serializing it |
| `internal/platform/server/middleware/auth.go` | Read `access_token` cookie first; fall back to `Authorization: Bearer` header (for Postman/Swagger) |
| `internal/platform/server/middleware/optional_auth.go` | Same fallback pattern as `auth.go` |
| `internal/platform/server/middleware/cors.go` | Add `AllowCredentials: true` |
| `docs/api_contract_refined.md` | Remove token fields from response schema; document cookie-based auth |

### Files NOT to change
- `internal/auth/service.go` — token generation logic unchanged
- `internal/auth/store.go` — unchanged
- `internal/auth/postgres.go` — unchanged
- All other modules — unchanged

---

## Detailed Change Spec

### Task 1: `internal/auth/dto.go`

```go
// BEFORE
type TokenResponse struct {
    AccessToken  string       `json:"access_token"`
    RefreshToken string       `json:"refresh_token"`
    ExpiresIn    int          `json:"expires_in"`
    User         UserResponse `json:"user"`
}

// AFTER — both tokens stripped from JSON, carried via json:"-" for handler use only
type TokenResponse struct {
    AccessToken  string       `json:"-"`           // set in cookie, never serialized
    RefreshToken string       `json:"-"`           // set in cookie, never serialized
    ExpiresIn    int          `json:"expires_in"`
    User         UserResponse `json:"user"`
}

// DELETE entire struct (refresh token comes from cookie now):
// type RefreshRequest struct { ... }
```

---

### Task 2: `internal/auth/handler.go`

**Add `setTokenCookies()` helper:**
```go
func (h *Handler) setTokenCookies(c *fiber.Ctx, accessToken, refreshToken string) {
    c.Cookie(&fiber.Cookie{
        Name:     "access_token",
        Value:    accessToken,
        HTTPOnly: true,
        Secure:   h.secureCookie, // false in dev, true in prod/staging
        SameSite: "Strict",
        Path:     "/api/v1",
        MaxAge:   900, // 15 minutes
    })
    c.Cookie(&fiber.Cookie{
        Name:     "refresh_token",
        Value:    refreshToken,
        HTTPOnly: true,
        Secure:   h.secureCookie,
        SameSite: "Strict",
        Path:     "/api/v1/auth/refresh", // scoped — only sent to refresh endpoint
        MaxAge:   604800, // 7 days
    })
}
```

**Add `clearTokenCookies()` helper:**
```go
func (h *Handler) clearTokenCookies(c *fiber.Ctx) {
    for _, name := range []string{"access_token", "refresh_token"} {
        c.Cookie(&fiber.Cookie{
            Name:     name,
            Value:    "",
            HTTPOnly: true,
            Secure:   h.secureCookie,
            SameSite: "Strict",
            MaxAge:   -1,
        })
    }
}
```

**`Login()` and `Register()` — set cookies after service call:**
```go
res, err := h.service.Login(c.Context(), req)
// ...
h.setTokenCookies(c, res.AccessToken, res.RefreshToken)
return response.Success(c, fiber.StatusOK, "Login successful", res) // res serializes without tokens (json:"-")
```

**`Refresh()` — read from cookie:**
```go
func (h *Handler) Refresh(c *fiber.Ctx) error {
    token := c.Cookies("refresh_token")
    if token == "" {
        return response.Error(c, apierror.ErrUnauthorized)
    }
    res, err := h.service.Refresh(c.Context(), token)
    if err != nil {
        return response.Error(c, err)
    }
    h.setTokenCookies(c, res.AccessToken, res.RefreshToken) // rotate both
    return response.Success(c, fiber.StatusOK, "Token refreshed successfully", map[string]int{"expires_in": res.ExpiresIn})
}
```

**`Logout()` — clear both cookies:**
```go
func (h *Handler) Logout(c *fiber.Ctx) error {
    h.clearTokenCookies(c)
    return response.Success(c, fiber.StatusOK, "Logged out successfully", nil)
}
```

---

### Task 3: `internal/platform/server/middleware/auth.go`

**Token extraction — cookie first, Authorization header fallback:**
```go
// Try cookie first (browser clients)
token := c.Cookies("access_token")

// Fall back to Authorization: Bearer header (Postman, Swagger, API clients)
if token == "" {
    auth := c.Get("Authorization")
    if strings.HasPrefix(auth, "Bearer ") {
        token = strings.TrimPrefix(auth, "Bearer ")
    }
}

if token == "" {
    return response.Error(c, apierror.ErrUnauthorized)
}
```

Apply the same pattern to `optional_auth.go`.

---

### Task 4: `internal/platform/server/middleware/cors.go`

```go
AllowCredentials: true,
// Required for browser to send cookies on cross-origin requests
```

> **Note:** `AllowOrigins` must be explicit (not `"*"`) when `AllowCredentials: true`. Example: `"http://localhost:3000,https://momijihome.com"`.

---

## Middleware Compatibility

The fallback to `Authorization: Bearer` in the middleware means:
- ✅ Browser (admin panel/storefront) → uses cookie automatically
- ✅ Postman → pass `Authorization: Bearer <token>` manually (works unchanged)
- ✅ Swagger UI → still works with BearerAuth scheme
- ✅ No breaking change for any existing integration

---

## Verification Plan

1. `POST /auth/login` → response body has NO token fields; response has `Set-Cookie` headers for both `access_token` and `refresh_token` with `HttpOnly` flag
2. Browser devtools → `Application > Cookies` → both cookies present; `document.cookie` shows neither (HttpOnly confirmed)
3. `GET /auth/me` with no `Authorization` header but cookie present → returns user profile ✅
4. `POST /auth/refresh` with no body, no header, cookie present → returns new `expires_in`; cookies rotated
5. `POST /auth/logout` → both cookies cleared (`Max-Age=-1` in response headers)
6. After logout: `GET /auth/me` → `401 Unauthorized` ✅
7. Postman: manually set `Authorization: Bearer <token>` → still works (fallback path)

---

## Note for Implementing Session

> Read `internal/platform/server/middleware/auth.go` and `optional_auth.go` FIRST.  
> The `Handler` struct in `internal/auth/handler.go` already has `secureCookie bool` field (wired in `main.go` line 96–97).  
> Use `json:"-"` tags on `AccessToken` and `RefreshToken` in `TokenResponse` — simplest approach, no new structs.  
> CORS `AllowOrigins` must list explicit origins (not `*`) when `AllowCredentials: true`.
