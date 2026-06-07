# Weekly Meeting — 2026-06-07 Changes Plan

> **Status:** 🔲 PENDING
> **Date:** 2026-06-07
> **Source:** Weekly meeting decisions

---

## Item 1 — Remove `fulfillment_type` from Shopify Sync Upsert

### Problem
Currently, `SyncFromShopify` derives `fulfillment_type` from `inventoryQuantity <= 0` and writes it to the DB on every sync. But `fulfillment_type` is an **admin-managed field** in our app — the admin explicitly sets it via `PATCH /products/:id/status`. Every time sync runs, it can silently reset admin overrides.

### Current Code Location
- `internal/product/service.go` lines ~279-292: builds `ft` from `InventoryQuantity` and sets `FulfillmentType` on the variant.
- `internal/product/postgres.go` line 110: `UpsertVariant` includes `"fulfillment_type"` in `DoUpdates`.

### Proposed Change

**File: `internal/product/service.go`**

Remove the `ft` / `FulfillmentType` derivation from the sync loop. The variant struct passed to `UpsertVariant` should NOT set `FulfillmentType`.

```go
// BEFORE
ft := FulfillmentTypeShipReady
if vNode.InventoryQuantity <= 0 {
    ft = FulfillmentTypePreOrder
}
variant := &ProductVariant{
    ...
    FulfillmentType:   string(ft),
    ...
}

// AFTER
variant := &ProductVariant{
    ProductID:              p.ID,
    ShopifyVariantID:       vNode.ID,
    Title:                  vNode.Title,
    SKU:                    vNode.Sku,
    Price:                  price,
    ImageSrc:               vNode.Image.Url,
    InventoryQuantity:      vNode.InventoryQuantity,
    ShopifyInventoryItemID: vNode.InventoryItem.ID,
    // FulfillmentType intentionally omitted — owned by app, not Shopify
}
```

**File: `internal/product/postgres.go`**

Remove `"fulfillment_type"` from the `DoUpdates` list in `UpsertVariant`.

```go
// BEFORE
DoUpdates: clause.AssignmentColumns([]string{
    "title", "sku", "price", "image_src",
    "inventory_quantity", "fulfillment_type",   // ← REMOVE THIS
    "shopify_inventory_item_id", "updated_at",
}),

// AFTER
DoUpdates: clause.AssignmentColumns([]string{
    "title", "sku", "price", "image_src",
    "inventory_quantity", "shopify_inventory_item_id", "updated_at",
}),
```

> **Note on new variants:** For NEW variants (first-time insert, no conflict), `fulfillment_type` will be empty string `""`. 
> The team must decide the default. Options:
> - Default to `"ship_ready"` in the DB column definition: `DEFAULT 'ship_ready'`
> - Or set it on INSERT only if not already set (check if variant already exists before syncing)
> - **Recommended:** Add a DB migration to set `DEFAULT 'ship_ready'` on `product_variants.fulfillment_type` so new variants get a safe default without app logic.

---

## Item 2 — Shipping: Move from Flat Rate to Third-Party Provider

### Context
The existing plan `docs/plans/2026-06-04-shipping-real-calculation.md` outlined **flat rate tiers** as the MVP approach. The meeting has changed this: **a real third-party shipping API will be used instead** to estimate shipping rates before checkout (mocking Shopify API rates).

The dimension storage part of the old plan (DB migration, admin input API, CSV import) **still applies and is required** as the provider will need dimensions to calculate rates.

### Revised Decision

| Original Plan | Revised Decision |
|---|---|
| Flat rate tier table (XS/S/M/L/XL) | ❌ Deprecated |
| Tier multipliers per method | ❌ Deprecated |
| Product dimension storage in DB | ✅ Still required (W×H×D and Weight) |
| Admin dimension input UI/API | ✅ Still required |
| CSV import/export for dimensions | ✅ Still required (see Item 3) |
| Real carrier API (UPS/FedEx/etc.) | ✅ NOW REQUIRED — third-party provider needed to match API rates |

### What Needs to be Decided (Open Questions)
> [!IMPORTANT]
> **The third-party shipping provider has not been selected yet.**
> Before this can be implemented, the team must confirm:
> 1. Which provider? (e.g., Shippo, EasyPost, RajaOngkir, JNE API, Lalamove, etc.)
> 2. What are the required inputs? (origin zip, destination zip, weight, dimensions)
> 3. What credentials/API key are needed?

### What This Plan Covers (Pre-Integration)
This plan prepares the infrastructure so integration can drop in cleanly:

1. **DB: Add dimension columns** to `product_variants` (still the same as the old plan)
2. **Store `FulfillmentType` as app-owned** (Item 1 above)
3. **Stub the shipping API call** — keep the endpoint returning mock data, but refactor it to call a `ShippingProvider` interface, so the real implementation can be swapped in without changing handler/service logic

```go
// internal/checkout/shipping_provider.go (NEW)
type ShippingProvider interface {
    GetRates(ctx context.Context, req ShippingRateRequest) ([]ShippingRate, error)
}

type ShippingRateRequest struct {
    OriginZip      string
    DestinationZip string
    WeightKg       float64
    WidthCm        float64
    HeightCm       float64
    DepthCm        float64
}

type ShippingRate struct {
    ProviderID       string  // e.g., "jne_reg", "jnt_express"
    Label            string  // display name
    EstimatedArrival string
    Cost             float64
    Currency         string
}
```

A `MockShippingProvider` that returns static rates will be used until the real provider is integrated.

---

## Item 3 — Product Dimension CSV Import/Export

### Feature Overview
- **Download Template:** `GET /api/v1/products/variants/dimensions/template` — returns a CSV file with ALL product variants listed, with dimension columns empty (or filled if already set).
- **Import CSV:** `POST /api/v1/products/variants/dimensions/import` — accepts a CSV file upload, parses it, and bulk-updates dimension fields.

### CSV Format

```csv
variant_id,product_title,variant_title,sku,weight_kg,width_cm,height_cm,depth_cm
gid://shopify/ProductVariant/123,Snowboard,Default Title,SKU-01,2.5,150,30,10
gid://shopify/ProductVariant/456,Helmet,Red / Large,SKU-02,0.5,25,25,25
```

- `variant_id` is the Shopify GID (used as the update key — never editable by admin)
- `product_title`, `variant_title`, `sku` are read-only context columns (admin fills in the blank dimension columns)
- Blank dimension values are skipped (not updated to 0)

### Files to Create/Modify

#### [NEW] `internal/product/handler.go` — two new handlers
- `DownloadDimensionTemplate` — queries all variants, generates CSV, returns with `Content-Disposition: attachment; filename="dimension-template.csv"`
- `ImportDimensions` — parses multipart CSV upload, bulk-updates dimensions

#### [MODIFY] `internal/product/service.go`
- `GetAllVariantsForCSV(ctx) ([]ProductVariant, error)` — returns all variants with product titles joined
- `BulkUpdateDimensions(ctx, rows []DimensionRow) error`

#### [MODIFY] `internal/product/store.go` + `internal/product/postgres.go`
- `BulkUpdateVariantDimensions(ctx, variants []DimensionUpdateInput) error`

#### [MODIFY] Route registration
```go
admin.Get("/variants/dimensions/template", h.DownloadDimensionTemplate)
admin.Post("/variants/dimensions/import", h.ImportDimensions)
```

---

## Item 4 — Prefill Shopify Checkout with Customer Info

### Problem
Currently `POST /checkout` only passes `email` to Shopify's `BuyerIdentity`. The user then has to re-enter their full address on the Shopify checkout page — a poor UX.

### Shopify Storefront API Support
The Shopify `cartCreate` mutation's `buyerIdentity` field supports a `deliveryAddressPreferences` array which can prefill the address. Reference: [Shopify Storefront API — CartBuyerIdentityInput](https://shopify.dev/docs/api/storefront/2024-01/input-objects/CartBuyerIdentityInput)

### New Checkout Request Body

```json
{
  "shipping_method": "ground",
  "email": "customer@example.com",
  "first_name": "John",
  "last_name": "Doe",
  "address1": "123 Main St",
  "city": "New York",
  "state": "NY",
  "zip": "10001",
  "country": "US",
  "phone": "+12025551234"
}
```

All fields except `email` are optional. If provided, they are passed to Shopify.

### Files to Modify

#### [MODIFY] `internal/checkout/dto.go` — `InitiateCheckoutRequest`
Add optional address fields:
```go
type InitiateCheckoutRequest struct {
    ShippingMethod string `json:"shipping_method" validate:"required"`
    Email          string `json:"email,omitempty"`
    FirstName      string `json:"first_name,omitempty"`
    LastName       string `json:"last_name,omitempty"`
    Address1       string `json:"address1,omitempty"`
    City           string `json:"city,omitempty"`
    State          string `json:"state,omitempty"`
    Zip            string `json:"zip,omitempty"`
    Country        string `json:"country,omitempty"`
    Phone          string `json:"phone,omitempty"`
}
```

#### [MODIFY] `internal/platform/shopify/client.go` — `CartBuyerIdentityInput`
Extend the struct to include delivery address preferences:
```go
type CartBuyerIdentityInput struct {
    Email                      string                       `json:"email,omitempty"`
    Phone                      string                       `json:"phone,omitempty"`
    DeliveryAddressPreferences []CartDeliveryAddressInput   `json:"deliveryAddressPreferences,omitempty"`
}

type CartDeliveryAddressInput struct {
    DeliveryAddress MailingAddressInput `json:"deliveryAddress"`
}

type MailingAddressInput struct {
    FirstName string `json:"firstName,omitempty"`
    LastName  string `json:"lastName,omitempty"`
    Address1  string `json:"address1,omitempty"`
    City      string `json:"city,omitempty"`
    Province  string `json:"province,omitempty"` // = state
    Zip       string `json:"zip,omitempty"`
    Country   string `json:"country,omitempty"`  // ISO 2-letter code
    Phone     string `json:"phone,omitempty"`
}
```

#### [MODIFY] `internal/checkout/service.go` — `InitiateCheckout`
Build the `BuyerIdentity` from request fields if address info is present:
```go
buyerIdentity := &shopify.CartBuyerIdentityInput{
    Email: req.Email,
    Phone: req.Phone,
}

if req.Address1 != "" {
    buyerIdentity.DeliveryAddressPreferences = []shopify.CartDeliveryAddressInput{
        {DeliveryAddress: shopify.MailingAddressInput{
            FirstName: req.FirstName,
            LastName:  req.LastName,
            Address1:  req.Address1,
            City:      req.City,
            Province:  req.State,
            Zip:       req.Zip,
            Country:   req.Country,
        }},
    }
}

cartInput.BuyerIdentity = buyerIdentity
```

---

## Item 5 — Redirect "Continue Shopping" to FE URL

### Problem
After Shopify payment is complete, the "Continue Shopping" button redirects to Shopify's default storefront, not our app.

### Solution
Shopify Storefront `cartCreate` supports a `note` or `attributes` to pass metadata, but the **return URL** is controlled by the `checkoutUrl` query parameters.

The Shopify checkout URL supports appending `?return_to=<url>` which redirects the user back to the specified page after checkout. However, the more reliable approach is to append it at cart creation time via the `attributes` field or at the checkout level.

**Recommended Approach: Append `?return_to` to the `checkoutUrl`** before returning it to the frontend:

```go
// In InitiateCheckout service, after getting checkoutUrl from Shopify:
if s.feURL != "" {
    checkoutUrl = res.CheckoutUrl + "?return_to=" + url.QueryEscape(s.feURL)
}
```

### Files to Modify

#### [MODIFY] `internal/config/config.go`
Add `FrontendURL` to `AppConfig`:
```go
type AppConfig struct {
    Env         string
    Port        string
    Host        string
    FrontendURL string  // NEW — from env FE_URL
}
```
Load from env:
```go
cfg.App.FrontendURL = os.Getenv("FE_URL")
```

#### [MODIFY] `internal/checkout/service.go`
Pass `feURL` to the service and append `?return_to`:
```go
type service struct {
    cartService      cart.CartService
    shopifyCli       shopify.Client
    stockLockService StockLockService
    feURL            string  // NEW
}
```

#### [MODIFY] `cmd/server/main.go`
Pass `cfg.App.FrontendURL` when constructing `CheckoutService`.

#### [MODIFY] `.env` / deployment environment
Add: `FE_URL=https://yourfrontend.com`

---

## Summary Table

| Item | Files | Complexity | Blocker? |
|------|-------|------------|----------|
| 1 | Remove `fulfillment_type` from Shopify sync upsert | Small | Add DB default `'ship_ready'` for new variants |
| 2 | Shipping third-party prep — define `ShippingProvider` interface + stub | Small | ⚠️ **Provider not selected yet** — needs PO decision |
| 3 | Dimension (W×H×D + Weight) CSV download template + import | Medium | DB migration for dimension columns |
| 4 | Prefill Shopify checkout with address fields | Small–Medium | ⏸ **Deferred** |
| 5 | `FE_URL` env → append `?return_to` to checkout URL | Small | None |
