# 2026-05-24 — Auth Token Security Strategy

> **Skill:** `scaffold/plan` (technical-constitution §Security)  
> **Scope:** Secure token delivery for `POST /auth/login`, `POST /auth/register`, `POST /auth/refresh`  
> **Authoritative contract:** `docs/api_contract_refined.md`  
> **Status:** 📋 Plan ready — pending implementation in next session

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

**The question:** Is this safe? Should `refresh_token` be in the body at all?

---

## Security Analysis

### Why `refresh_token` in the JSON body is risky for browser clients

| Risk | Detail |
|------|--------|
| **XSS exposure** | Any JavaScript running on the page (including injected malicious scripts) can read `response.data.refresh_token` if stored in `localStorage` or `sessionStorage` |
| **Long blast radius** | Access token TTL = 15 min (manageable). Refresh token TTL = 7 days. A stolen refresh token gives an attacker a 7-day silent session |
| **Log leakage** | API gateways, proxies, browser network tab recordings, or frontend error tracking (Sentry) that log response bodies will capture the refresh token |
| **Mobile/non-browser clients** | JSON body is **fine** for native mobile apps (iOS Keychain / Android Keystore) — these are isolated from XSS |

### Why `access_token` in the JSON body is acceptable

- TTL = 15 minutes: stolen token has a very small window
- Needed directly by the client for every `Authorization: Bearer <token>` header
- No practical way to deliver it via HttpOnly cookie while remaining usable by JS for API calls

### Current logout is broken

Looking at `handler.go` line 149–151:
```go
// With tokens stored in the body/local storage, logout is primarily a client-side action.
```

This comment reveals the issue: **server-side logout is impossible** when the refresh token lives only in the client's local storage. There is no server state to invalidate. A user who logs out still has a valid 7-day refresh token.

---

## Decision: Hybrid Strategy

**Access token** → JSON body (unchanged, required for Authorization header)  
**Refresh token** → `HttpOnly` cookie only (removed from JSON body)

This is the standard RFC 6749 / OAuth 2.0 best practice for browser-based clients.

### Cookie configuration
```
Set-Cookie: refresh_token=<token>; 
  HttpOnly;           ← JS cannot read it (XSS-proof)
  Secure;             ← HTTPS only (production)
  SameSite=Strict;    ← CSRF protection (only sent to same origin)
  Path=/api/v1/auth/refresh;  ← scoped only to refresh endpoint
  Max-Age=604800      ← 7 days in seconds
```

### New `TokenResponse` (JSON body, no refresh_token field)
```json
{
  "data": {
    "access_token": "eyJ...",
    "expires_in": 900,
    "user": {
      "id": "...",
      "email": "...",
      "role": "...",
      "shopify_customer_id": "..."
    }
  }
}
```

### New `POST /auth/refresh` request (no body required)
The refresh token is automatically sent by the browser via cookie.  
The `refresh_token` body field in `RefreshRequest` is **removed**.  
The handler reads: `c.Cookies("refresh_token")`.

### Logout becomes real
With the cookie approach, `POST /auth/logout` can now **clear the cookie server-side**:
```go
c.Cookie(&fiber.Cookie{
    Name:     "refresh_token",
    Value:    "",
    Expires:  time.Now().Add(-time.Hour),
    HTTPOnly: true,
    Secure:   true,
    SameSite: "Strict",
    Path:     "/api/v1/auth/refresh",
})
```

---

## Trade-offs

| | JSON body (current) | HttpOnly Cookie (proposed) |
|--|---------------------|---------------------------|
| XSS protection | ❌ No | ✅ Yes |
| CSRF protection | ✅ Not applicable | ✅ SameSite=Strict |
| Mobile app support | ✅ Works natively | ⚠️ Needs custom header approach |
| Server-side logout | ❌ Impossible | ✅ Clear cookie |
| Postman/API testing | ✅ Easy | ⚠️ Cookie must be enabled in Postman |
| Implementation complexity | Low | Medium |

### Mobile app note
If a mobile app (React Native / Flutter) is added later, cookies work but require configuring the HTTP client to handle cookies (e.g., `CookieJar` in Go, `CookieStore` in Android). This is solvable. Mobile apps are not mentioned in the current PRD.

---

## What Needs to Change

### Files to modify

| File | Change |
|------|--------|
| `internal/auth/dto.go` | Remove `RefreshToken` field from `TokenResponse`; remove `RefreshRequest` struct (no longer needed) |
| `internal/auth/handler.go` | `Login()` + `Register()` → set HttpOnly cookie after calling service; `Refresh()` → read from `c.Cookies("refresh_token")` instead of request body; `Logout()` → clear the cookie |
| `internal/auth/service.go` | `Refresh()` signature changes: accepts token string (unchanged); returns only `TokenResponse` without refresh_token field (unchanged internally) |
| `internal/platform/server/middleware/cors.go` | Ensure `AllowCredentials: true` so browser sends cookies cross-origin (required for cookie-based auth) |
| `docs/api_contract_refined.md` | Update `TokenResponse` schema; update `POST /auth/refresh` request to show no body needed |

### Files NOT to change
- `internal/auth/service.go` logic — token generation is unchanged
- `internal/auth/store.go` — unchanged
- `internal/auth/postgres.go` — unchanged
- All other modules — unchanged

---

## Detailed Change Spec

### Task 1: `internal/auth/dto.go`

**Remove** `RefreshToken` from `TokenResponse`:
```go
// BEFORE
type TokenResponse struct {
    AccessToken  string       `json:"access_token"`
    RefreshToken string       `json:"refresh_token"`   // ← REMOVE THIS
    ExpiresIn    int          `json:"expires_in"`
    User         UserResponse `json:"user"`
}

// AFTER
type TokenResponse struct {
    AccessToken string       `json:"access_token"`
    ExpiresIn   int          `json:"expires_in"`
    User        UserResponse `json:"user"`
}
```

**Remove** `RefreshRequest` struct (refresh token now comes from cookie, no body needed):
```go
// DELETE ENTIRE STRUCT:
type RefreshRequest struct {
    RefreshToken string `json:"refresh_token" validate:"required"`
}
```

---

### Task 2: `internal/auth/handler.go`

**`setRefreshCookie()` — new private helper:**
```go
func setRefreshCookie(c *fiber.Ctx, token string, ttl time.Duration) {
    c.Cookie(&fiber.Cookie{
        Name:     "refresh_token",
        Value:    token,
        HTTPOnly: true,
        Secure:   true,   // set false only in development
        SameSite: "Strict",
        Path:     "/api/v1/auth/refresh",
        MaxAge:   int(ttl.Seconds()),
    })
}
```

> In development (`APP_ENV != "production"`), set `Secure: false` so it works over HTTP. Read from `cfg.App.Env`.

**`Login()` — set cookie after service call:**
```go
// After: res, err := h.service.Login(...)
setRefreshCookie(c, res.RefreshToken, 7*24*time.Hour)
// res.RefreshToken must be populated internally but stripped from the returned DTO
```

> The service still returns a full internal token struct. The handler sets the cookie and returns only `TokenResponse` (without refresh_token).

**`Register()` — same as Login:**
```go
setRefreshCookie(c, res.RefreshToken, 7*24*time.Hour)
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

    setRefreshCookie(c, res.RefreshToken, 7*24*time.Hour) // rotate cookie
    return response.Success(c, fiber.StatusOK, "Token refreshed successfully", res)
}
```

**`Logout()` — clear cookie:**
```go
func (h *Handler) Logout(c *fiber.Ctx) error {
    c.Cookie(&fiber.Cookie{
        Name:     "refresh_token",
        Value:    "",
        HTTPOnly: true,
        Secure:   true,
        SameSite: "Strict",
        Path:     "/api/v1/auth/refresh",
        MaxAge:   -1, // immediate expiry
    })
    return response.Success(c, fiber.StatusOK, "Logged out successfully", nil)
}
```

---

### Task 3: Internal token struct refactor

The service currently returns `TokenResponse` which will no longer have `RefreshToken`.  
The handler needs the refresh token **to set the cookie** but the DTO no longer carries it.

**Option A (recommended):** Add an internal-only struct the service returns:
```go
// internal/auth/service.go — internal only, not exported as JSON
type tokenPair struct {
    AccessToken  string
    RefreshToken string // used by handler for cookie, never serialized
    ExpiresIn    int
    User         UserResponse
}
```

The handler uses `tokenPair.RefreshToken` to set the cookie, then constructs `TokenResponse` (without `RefreshToken`) for the JSON response.

**Option B:** Keep `RefreshToken` in `TokenResponse` but add `json:"-"` tag:
```go
RefreshToken string `json:"-"` // never serialized; handler reads this for cookie
```

> Recommendation: **Option B** is simpler — fewer structs, the `-` tag ensures it never appears in JSON output, the handler can still access it via `res.RefreshToken`.

---

### Task 4: `internal/platform/server/middleware/cors.go`

Ensure credentials are allowed (required for cookie cross-origin requests):
```go
AllowCredentials: true,
```

Without this, the browser will not send cookies on cross-origin requests (e.g., frontend on `localhost:3001` calling API on `localhost:3000`).

---

### Task 5: `docs/api_contract_refined.md` (documentation update)

Update the `TokenResponse` schema to remove `refresh_token`.  
Update `POST /auth/refresh` to note: "No body required. Refresh token is read from `refresh_token` HttpOnly cookie."  
Add a section: **Token Storage Guide**:
- Browser clients: access_token stored in memory (React state/context) — NOT localStorage
- Refresh token: stored automatically via HttpOnly cookie, managed by browser

---

## Verification Plan

1. `POST /auth/login` → response body has NO `refresh_token` field; response headers have `Set-Cookie: refresh_token=...; HttpOnly`
2. `POST /auth/refresh` with no body but cookie present → returns new `access_token`, rotates cookie
3. `POST /auth/refresh` with no body and no cookie → returns `401`
4. `POST /auth/logout` → response headers have `Set-Cookie: refresh_token=; Max-Age=-1` (cleared)
5. Browser devtools: confirm `refresh_token` cookie is NOT readable via `document.cookie` (HttpOnly)
6. Postman: enable "Send cookies" — verify refresh flow works end-to-end

---

## Note for Implementing Session

> Read `internal/auth/handler.go` and `internal/auth/dto.go` first before making changes.  
> The internal service does not need to change its logic — only the handler layer and DTO layer change.  
> Use `json:"-"` tag on `RefreshToken` in `TokenResponse` (Option B above) — minimal diff, no new structs.  
> The `Secure: true` flag on the cookie should respect `APP_ENV`: set `false` in development, `true` in production.
