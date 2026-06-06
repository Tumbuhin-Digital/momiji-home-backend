# Guest Checkout — Remove Mandatory Login Requirement

> **Status:** 🔲 PENDING
> **Date:** 2026-06-06
> **PO Decision:** Login is no longer mandatory at checkout.
>   Guest users must provide their email in the checkout request body instead.
> **Effort:** Small — handler.go only, ~15 lines changed

---

## Background

The PRD originally mandated "Login wajib sebelum checkout". The PO has revised this:
guest users may proceed to checkout without logging in, as long as they provide an email address.
The email is used by the webhook (`orders/paid`) to create or link a customer account automatically.

---

## What Is Already Implemented (No Changes Needed)

| Item | Location | Status |
|------|----------|--------|
| `email` field in `InitiateCheckoutRequest` | `dto.go:62` | ✅ Already exists |
| Service passes `req.Email` to Shopify `BuyerIdentity` | `service.go:80-82` | ✅ Already works |
| Webhook creates/links account from email on `orders/paid` | `webhook/service.go:84-98` | ✅ Already works |
| `CheckoutService.InitiateCheckout` accepts `userID *string` (nil-safe) | `service.go:27` | ✅ Already nil-safe |
| `GET /checkout/confirm` — no user context required | `handler.go:192-213` | ✅ Already safe for guest |

---

## Root Cause of Current Block

**Two problems in `internal/checkout/handler.go`:**

### Problem 1 — Hard `middleware.Auth` on routes

```go
// handler.go:29 and 35 — current
shipping.Use(middleware.Auth(h.jwtSecret))   // rejects guest with 401
checkout.Use(middleware.Auth(h.jwtSecret))   // rejects guest with 401
```

Must change to `middleware.OptionalAuth` so guests with `X-Session-ID` can also pass.

### Problem 2 — Unsafe type assertion on `user_id`

```go
// handler.go:105 and 174 — current
uid := c.Locals("user_id").(string)  // PANICS if no login (nil → string assertion)
```

When a guest hits these endpoints after the middleware fix, `c.Locals("user_id")` returns `nil`.
The `.(string)` assertion on nil panics the server.

---

## Proposed Changes — `internal/checkout/handler.go` Only

### Change 1 — Switch middleware from Auth to OptionalAuth (lines 29, 35)

```go
// BEFORE
shipping.Use(middleware.Auth(h.jwtSecret))
// ...
checkout.Use(middleware.Auth(h.jwtSecret))

// AFTER
shipping.Use(middleware.OptionalAuth(h.jwtSecret))
// ...
checkout.Use(middleware.OptionalAuth(h.jwtSecret))
```

### Change 2 — Safe identity extraction helper (new private function)

Add a helper at the top of the handler file to safely extract user_id and session_id:

```go
func (h *Handler) extractIdentity(c *fiber.Ctx) (userID *string, sessionID *string) {
    if uid, ok := c.Locals("user_id").(string); ok && uid != "" {
        userID = &uid
    }
    if sid, ok := c.Locals("session_id").(string); ok && sid != "" {
        sessionID = &sid
    }
    return
}
```

This mirrors the same pattern already used in `cart/handler.go:55-64`.

### Change 3 — `GetCheckoutSummary` — use safe extraction + pass session

```go
// BEFORE
uid := c.Locals("user_id").(string)
cartRes, err := h.cartService.GetCartResponse(c.Context(), &uid, nil)

// AFTER
uid, sid := h.extractIdentity(c)
cartRes, err := h.cartService.GetCartResponse(c.Context(), uid, sid)
```

### Change 4 — `InitiateCheckout` — use safe extraction + guest email validation

```go
// BEFORE
uid := c.Locals("user_id").(string)
res, err := h.checkoutService.InitiateCheckout(c.Context(), &uid, nil, req)

// AFTER
uid, sid := h.extractIdentity(c)

// Guest must provide email if not logged in
if uid == nil && req.Email == "" {
    return response.Error(c, apierror.New(400, "validation_error", "email is required for guest checkout"))
}

res, err := h.checkoutService.InitiateCheckout(c.Context(), uid, sid, req)
```

---

## No Changes Needed In

| File | Reason |
|------|--------|
| `checkout/service.go` | Already nil-safe, already passes email |
| `checkout/dto.go` | `email` field already exists |
| `checkout/store.go` | No change |
| `checkout/stocklock.go` | Already accepts `userID *string` and `sessionID *string` |
| `webhook/service.go` | Already creates account from email on payment |
| `cmd/server/main.go` | No new dependencies |
| DB migrations | None needed |

---

## Frontend Impact

Guest checkout request body for `POST /checkout` must include `email`:

```json
{
  "shipping_method": "ground",
  "email": "customer@example.com"
}
```

For logged-in users, `email` is optional (backend resolves from JWT).

For shipping endpoints (`GET /shipping/methods`, `POST /shipping/calculate`):
- Now also accept `X-Session-ID` — no breaking change for logged-in users.

---

## Verification Plan

```bash
# 1. Guest creates session
curl -X POST http://localhost:3000/api/v1/cart/session
# → gets session_id

# 2. Guest adds item
curl -X POST http://localhost:3000/api/v1/cart/items \
  -H "X-Session-ID: sess_xxx" \
  -d '{"variant_id": "gid://shopify/ProductVariant/...", "quantity": 1}'

# 3. Guest gets shipping methods (should now work without Bearer)
curl http://localhost:3000/api/v1/shipping/methods \
  -H "X-Session-ID: sess_xxx"
# Expected: 200 OK

# 4. Guest checkout summary (should now work without Bearer)
curl -X POST http://localhost:3000/api/v1/checkout/summary \
  -H "X-Session-ID: sess_xxx" \
  -d '{"shipping_method": "ground"}'
# Expected: 200 OK

# 5. Guest initiates checkout WITHOUT email (should be rejected)
curl -X POST http://localhost:3000/api/v1/checkout \
  -H "X-Session-ID: sess_xxx" \
  -d '{"shipping_method": "ground"}'
# Expected: 400 validation_error "email is required for guest checkout"

# 6. Guest initiates checkout WITH email (should succeed)
curl -X POST http://localhost:3000/api/v1/checkout \
  -H "X-Session-ID: sess_xxx" \
  -d '{"shipping_method": "ground", "email": "test@example.com"}'
# Expected: 200 OK with checkout_url

# 7. Logged-in user initiates checkout WITHOUT email (should still work)
curl -X POST http://localhost:3000/api/v1/checkout \
  -H "Authorization: Bearer <token>" \
  -d '{"shipping_method": "ground"}'
# Expected: 200 OK (email resolved from JWT)
```
