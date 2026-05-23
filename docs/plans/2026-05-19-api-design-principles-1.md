# 2026-05-19 — API Design Principles (Phase 2.5, 3 & 4)

> **Skill:** `api-design-principles` (technical-constitution §API Design Principles)  
> **Phases covered:** Phase 2.5 (refined contract), Phase 3 (Cart/Checkout), Phase 4 (Products/Orders)  
> **Authoritative source:** `docs/api_contract_refined.md`  
> **Status:** ✅ Complete (partial gaps noted)

---

## Response Envelope Standard

All responses use the unified envelope format (updated in Phase 2.5):

```json
// Success
{
  "status": "success",
  "message": "Human-readable message",
  "data": { ... },
  "timestamp": "2026-05-19T13:31:16Z"
}

// Error
{
  "status": "error",
  "message": "Human-readable error",
  "error": {
    "code": "snake_case_code",
    "details": { "field": "reason" }
  },
  "timestamp": "2026-05-19T13:31:16Z"
}
```

**Implementation:** `internal/shared/response/envelope.go` — `response.Success()` and `response.Error()`

---

## Error Code Standard

All error codes are `snake_case` strings (not numeric codes):
- `validation_error` — 400, field-level errors in `details` map
- `unauthorized` — 401
- `forbidden` — 403
- `not_found` — 404
- `conflict` — 409
- `internal_error` — 500

**Implementation:** `internal/shared/apierror/errors.go`

---

## Endpoint Inventory

### Auth (`/api/v1/auth`)
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/auth/register` | None | DEV ONLY — temporary |
| POST | `/auth/login` | None | Returns token pair |
| POST | `/auth/refresh` | None | Rotate tokens |
| POST | `/auth/logout` | Bearer | Client-side clear |
| GET | `/auth/me` | Bearer | Current user profile |

### Cart (`/api/v1/cart`)
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/cart/session` | None | Create guest session |
| GET | `/cart` | Optional | Get cart (split by type) |
| GET | `/cart/summary` | Optional | Totals + deposit amounts |
| POST | `/cart/items` | Optional | Add item |
| PATCH | `/cart/items/:id` | Optional | Update qty |
| DELETE | `/cart/items/:id` | Optional | Remove item |
| DELETE | `/cart` | Optional | Clear cart |
| POST | `/cart/merge` | Bearer | Merge guest → auth cart on login |

### Checkout (`/api/v1/checkout`)
| Method | Path | Auth | Status |
|--------|------|------|--------|
| POST | `/checkout/summary` | Optional | ⚠️ Partial — hardcoded shipping |
| GET | `/shipping/methods` | Optional | ❌ Not built |
| POST | `/shipping/calculate` | Optional | ❌ Not built |

### Products (`/api/v1/products`)
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/products` | None | List all products |
| GET | `/products/:id` | None | Product detail |
| GET | `/products/:id/variants` | None | Product variants |
| POST | `/products/sync` | Admin | Sync from Shopify Admin API |
| PATCH | `/products/:id/status` | Admin | Update product status |
| PATCH | `/products/:id/batch` | Admin | Update batch label |
| PATCH | `/products/variant/:id/price` | Admin | Override ws_price |

### Orders (`/api/v1/orders`)
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/orders` | Optional | Create order (guest or auth) |
| GET | `/orders` | Bearer | List user's orders |
| GET | `/orders/:id` | Bearer | Order detail |
| PATCH | `/orders/:id/accept` | Bearer | Accept order |
| PATCH | `/orders/:id/cancel` | Bearer | Cancel order |
| PATCH | `/orders/:id/items/:itemId/step` | Bearer | Update fulfillment step |
| PATCH | `/orders/:id/items/:itemId/received` | Bearer | Mark item received |

### Pre-orders (`/api/v1/preorders`) — Phase 5, not started
| Method | Path | Auth |
|--------|------|------|
| GET | `/preorders` | Admin |
| GET | `/preorders/settlements/:id` | Admin |
| PATCH | `/preorders/settlements/:id/invoice` | Admin |
| PATCH | `/preorders/settlements/:id/paid` | Admin |

---

## Key Design Decisions

### Guest Session Pattern
- `X-Session-ID: <uuid>` header identifies anonymous carts
- `POST /cart/session` generates and returns a session ID
- `optional_auth` middleware resolves either `user_id` (from JWT) or `session_id` (from header) into Fiber locals
- Cart items linked by `session_id` (nullable) or `user_id` (nullable)

### Cart Split by Fulfillment Type
Cart response always separates items:
```json
{
  "ship_ready": [ { "variant_id": "...", "ws_price": "...", ... } ],
  "pre_order":  [ { "variant_id": "...", "ws_price": "...", "deposit_amount": "...", ... } ]
}
```
- `deposit_amount` = 50% of `ws_price × quantity` for pre_order items

### Order Creation Flow
```
POST /orders
  ├── if ship_ready items → Shopify Storefront API → checkout_url
  └── if pre_order items → Shopify Admin API → draft_order (invoice URL, 50% deposit)
```

---

## Swagger / OpenAPI
- Generated via `swag init -g cmd/server/main.go --parseDependency --parseInternal`
- UI at `GET /swagger/index.html`
- Stored in `docs/swagger.json`, `docs/swagger.yaml`, `docs/docs.go` (gitignored except these 3)
- Re-generate after any handler annotation change: `swag init -g cmd/server/main.go`
