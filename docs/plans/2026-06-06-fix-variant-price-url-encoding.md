# Fix Variant Price Server 400 Error (URL Encoding Issue)

> **Status:** 🔲 PENDING
> **Date:** 2026-06-06
> **Problem:** 
> Calling `PATCH /api/v1/products/variant/:id/price` with a Shopify GID (`gid://...`) causes a `400 Bad Request` with **no response body** on the server.
> This happens because the `%2F` (slashes) and `%3A` (colon) in the URL path trigger the server's Reverse Proxy/WAF (e.g., NGINX, Cloudflare) security rules, causing the proxy to block the request before it even reaches the Fiber backend.

---

## Proposed Solution
Stop sending the Shopify Variant ID via the URL path. Instead, pass it safely inside the JSON request body. 

### API Change (For Frontend Team)

**Old Request:**
```http
PATCH /api/v1/products/variant/gid%3A%2F%2Fshopify%2FProductVariant%2F48513248592127/price
{
  "ws_price": 100000,
  "retail_price": 120000
}
```

**New Request:**
```http
PATCH /api/v1/products/variant/price
{
  "variant_id": "gid://shopify/ProductVariant/48513248592127",
  "ws_price": 100000,
  "retail_price": 120000
}
```

---

## Implementation Plan

### 1. Update DTO
**File:** `internal/product/dto.go`
Update `UpdateVariantPriceRequest` to include `VariantID`:

```go
type UpdateVariantPriceRequest struct {
    VariantID   string   `json:"variant_id" validate:"required"`
    WSPrice     *float64 `json:"ws_price" validate:"omitempty,min=0"`
    RetailPrice *float64 `json:"retail_price" validate:"omitempty,min=0"`
}
```

### 2. Update Route
**File:** `internal/product/handler.go`
Change the route definition (around line 36):

```go
// BEFORE
admin.Patch("/variant/:id/price", h.UpdateVariantPrice)

// AFTER
admin.Patch("/variant/price", h.UpdateVariantPrice)
```

### 3. Update Handler Logic
**File:** `internal/product/handler.go`
Update `UpdateVariantPrice` function to read ID from the request body instead of the URL path:

```go
// BEFORE
func (h *Handler) UpdateVariantPrice(c *fiber.Ctx) error {
    variantID, _ := url.PathUnescape(c.Params("id"))
    slog.InfoContext(c.Context(), "UpdateVariantPrice", slog.String("variant_id", variantID))
    var req UpdateVariantPriceRequest
    if err := c.BodyParser(&req); err != nil {
        return response.Error(c, err)
    }
    if err := h.service.UpdateVariantPrice(c.Context(), variantID, req.WSPrice, req.RetailPrice); err != nil {
        return response.Error(c, err)
    }
    // ...

// AFTER
// @Router /products/variant/price [patch]
func (h *Handler) UpdateVariantPrice(c *fiber.Ctx) error {
    var req UpdateVariantPriceRequest
    if err := c.BodyParser(&req); err != nil {
        return response.Error(c, err)
    }
    if err := validator.ValidateStruct(&req); err != nil {
        return response.Error(c, err)
    }
    
    slog.InfoContext(c.Context(), "UpdateVariantPrice", slog.String("variant_id", req.VariantID))
    
    if err := h.service.UpdateVariantPrice(c.Context(), req.VariantID, req.WSPrice, req.RetailPrice); err != nil {
        return response.Error(c, err)
    }
    
    return response.Success(c, fiber.StatusOK, "Variant price updated", nil)
}
```
