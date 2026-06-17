# Plan: Bug Fixes + ShipStation Integration
*2026-06-17*

---

## Bug Fix 1: Excel Export Error 500 (Preorder + Orders)

### Root Cause Analysis

**Order Excel (500):** `ExportOrdersToExcel` in `internal/order/service.go` calls `s.GetOrders(ctx, "", query)` with `Limit = 1000000`. The underlying `GetOrdersByCustomer` uses GORM's `.Limit(1_000_000)` and `.Offset(0)`. This is not an SQL problem — the likely cause is that the `ExportOrdersToExcel` method is defined on the service but **never wired to a handler/route**. There is no `ExportOrdersToExcel` handler in `internal/order/handler.go`.

**Preorder Excel (500):** Same pattern — `ExportPreordersToExcel` exists in the service but must be checked if the handler is calling it correctly and the route responds with the proper `Content-Type` header.

### Fix

#### [MODIFY] `internal/order/handler.go`
Add the missing export handler and route:

```go
// In SetupRoutes, under admin group:
admin.Get("/export", h.ExportOrders)

// Handler:
func (h *Handler) ExportOrders(c *fiber.Ctx) error {
    var q OrderQuery
    if err := c.QueryParser(&q); err != nil {
        return response.Error(c, err)
    }
    data, err := h.service.ExportOrdersToExcel(c.Context(), q)
    if err != nil {
        return response.Error(c, err)
    }
    c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
    c.Set("Content-Disposition", `attachment; filename="orders.xlsx"`)
    return c.Send(data)
}
```

Also fix `ExportOrdersToExcel` in service.go — remove the `1000000` limit hack and use a real "no limit" approach:
```go
// Instead of limit 1000000, fetch all with no limit:
err := query.Preload("Items").Preload("Customer").Order("created_at DESC").Find(&orders).Error
```

#### Verify Preorder Export Route
Confirm `GET /preorders/export` is registered (it is, in `handler.go` line 27) and that the `ExportPreorders` handler sets the correct Content-Type header (it does). If still 500, add error logging to surface the actual panic/error.

---

## Bug Fix 2: New Shopify Products Not Syncing

### Root Cause Analysis

Looking at `internal/product/service.go`:
```go
const shopifySyncPageCap = 10  // max 10 pages × 50 products = 500 products
```

The sync queries `products(first: 50, after: $cursor)` — paginating up to 500 products. If you have **more than 500 products** in Shopify, the newest ones won't sync.

But a more likely bug: the GraphQL query **doesn't sort by `created_at DESC`**. Shopify's default sort is `id ASC` (oldest first). So with 500 products, the newest ones added after the 500-product mark are silently skipped.

### Fix

#### [MODIFY] `internal/product/service.go`

Two changes:
1. Add `sortKey: CREATED_AT, reverse: true` to fetch newest first
2. Increase the page cap or remove it for full sync

```go
// Change the query from:
products(first: 50, after: $cursor) {

// To:
products(first: 50, after: $cursor, sortKey: UPDATED_AT, reverse: false) {
```

> Using `UPDATED_AT` (ascending) ensures we catch recently created AND recently modified products.  
> Alternatively, add a **delta sync** that uses `query: "updated_at:>YYYY-MM-DD"` to only fetch products changed since the last sync.

Immediate pragmatic fix (raise cap and use `CREATED_AT` descending to catch new ones):
```go
const shopifySyncPageCap = 50  // 50 × 50 = 2500 products max
```

And change the query to always include the most recently added:
```go
query($cursor: String) {
  products(first: 50, after: $cursor, sortKey: CREATED_AT, reverse: true) {
```

---

## New Feature: ShipStation API v2 SDK + Rate Calculation

### Overview

We build a **thin internal Go SDK** for ShipStation API v2 (no 3rd party packages). Use it to power a new `GET /checkout/shipping-rates` endpoint that returns live quotes for pre-order items.

### Authentication

ShipStation API v2 uses **Bearer token** auth:
```
Authorization: Bearer {SHIPSTATION_SANDBOX_API_KEY}
```
Base URL (sandbox): `https://ssapi.shipstation.com` (v1 legacy) or `https://api.shipstation.com` (v2).

> **Confirm**: The `.env.template` has `SHIPSTATION_SANDBOX_API_KEY`. We need to confirm the base URL for v2. The v2 endpoint for rates is `POST /v2/rates/`.

---

### Part A: ShipStation SDK

#### [NEW] `internal/platform/shipstation/client.go`

```go
package shipstation

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

const (
    defaultBaseURL = "https://api.shipstation.com"
    sandboxBaseURL = "https://ssapi.shipstation.com" // confirm this
)

type Client interface {
    GetRates(ctx context.Context, req RateRequest) (*RateResponse, error)
    ListCarriers(ctx context.Context) ([]Carrier, error)
}

type client struct {
    baseURL    string
    apiKey     string
    httpClient *http.Client
}

func NewClient(apiKey string, sandbox bool) Client {
    base := defaultBaseURL
    if sandbox {
        base = sandboxBaseURL
    }
    return &client{
        baseURL: base,
        apiKey:  apiKey,
        httpClient: &http.Client{Timeout: 15 * time.Second},
    }
}

func (c *client) do(ctx context.Context, method, path string, body interface{}, out interface{}) error {
    var reqBody []byte
    if body != nil {
        var err error
        reqBody, err = json.Marshal(body)
        if err != nil {
            return fmt.Errorf("shipstation: marshal request: %w", err)
        }
    }

    req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(reqBody))
    if err != nil {
        return fmt.Errorf("shipstation: create request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+c.apiKey)
    req.Header.Set("Content-Type", "application/json")

    res, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("shipstation: http request: %w", err)
    }
    defer res.Body.Close()

    if res.StatusCode >= 400 {
        return fmt.Errorf("shipstation: api error status %d", res.StatusCode)
    }

    return json.NewDecoder(res.Body).Decode(out)
}
```

#### [NEW] `internal/platform/shipstation/dto.go`

```go
package shipstation

// --- Rate Request ---

type RateRequest struct {
    RateOptions RateOptions `json:"rate_options"`
    Shipment    Shipment    `json:"shipment"`
}

type RateOptions struct {
    CarrierIDs []string `json:"carrier_ids"`
}

type Shipment struct {
    ValidateAddress string    `json:"validate_address"` // "no_validation"
    ShipFrom        Address   `json:"ship_from"`
    ShipTo          Address   `json:"ship_to"`
    Packages        []Package `json:"packages"`
}

type Address struct {
    PostalCode  string `json:"postal_code"`
    CountryCode string `json:"country_code"`  // "US"
    CityLocality string `json:"city_locality,omitempty"`
    StateProvince string `json:"state_province,omitempty"`
}

type Package struct {
    Weight     Weight     `json:"weight"`
    Dimensions *Dimensions `json:"dimensions,omitempty"`
}

type Weight struct {
    Value float64 `json:"value"`
    Unit  string  `json:"unit"` // "pound", "ounce", "kilogram", "gram"
}

type Dimensions struct {
    Unit   string  `json:"unit"`   // "inch", "centimeter"
    Length float64 `json:"length"`
    Width  float64 `json:"width"`
    Height float64 `json:"height"`
}

// --- Rate Response ---

type RateResponse struct {
    Rates []Rate `json:"rates"`
}

type Rate struct {
    RateID           string  `json:"rate_id"`
    CarrierID        string  `json:"carrier_id"`
    ServiceCode      string  `json:"service_code"`
    CarrierFriendlyName string `json:"carrier_friendly_name"`
    ServiceType      string  `json:"service_type"`
    ShippingAmount   Money   `json:"shipping_amount"`
    DeliveryDays     *int    `json:"delivery_days"`
    EstimatedDeliveryDate string `json:"estimated_delivery_date"`
    Negotiated       bool    `json:"negotiated_rate"`
}

type Money struct {
    Currency string  `json:"currency"`
    Amount   float64 `json:"amount"`
}

// --- Carrier ---

type Carrier struct {
    CarrierID   string `json:"carrier_id"`
    FriendlyName string `json:"friendly_name"`
    Code        string `json:"code"`
}
```

#### [NEW] `internal/platform/shipstation/rates.go`

```go
func (c *client) GetRates(ctx context.Context, req RateRequest) (*RateResponse, error) {
    var res RateResponse
    if err := c.do(ctx, http.MethodPost, "/v2/rates/", req, &res); err != nil {
        return nil, err
    }
    return &res, nil
}

func (c *client) ListCarriers(ctx context.Context) ([]Carrier, error) {
    var res struct {
        Carriers []Carrier `json:"carriers"`
    }
    if err := c.do(ctx, http.MethodGet, "/v2/carriers/", nil, &res); err != nil {
        return nil, err
    }
    return res.Carriers, nil
}
```

---

### Part B: Config Integration

#### [MODIFY] `internal/config/config.go`
Add `ShipStation` config struct:
```go
type ShipStationConfig struct {
    APIKey  string
    Sandbox bool
}
```

In `Load()`:
```go
cfg.ShipStation.APIKey  = os.Getenv("SHIPSTATION_SANDBOX_API_KEY")
cfg.ShipStation.Sandbox = cfg.App.Env != "production"
```

#### [MODIFY] `.env.template`
Already has `SHIPSTATION_SANDBOX_API_KEY`. Also add:
```
SHIPSTATION_CARRIER_ID=   # e.g. se-XXXXXX (your Unishippers carrier ID from ListCarriers)
WAREHOUSE_ZIP=07055       # Origin ZIP (Passaic, NJ from Unishippers invoice)
WAREHOUSE_COUNTRY=US
```

---

### Part C: Checkout Service — Rate Endpoint

#### [MODIFY] `internal/checkout/service.go`
Add to `CheckoutService` interface:
```go
GetShippingRates(ctx context.Context, userID, sessionID *string, destZip, destCountry string) ([]ShippingRateDTO, error)
```

Implementation: 
1. Get cart items (pre-order only — ship_ready is handled by Shopify)
2. Aggregate total weight (KG → LB) and take the largest single-item dims
3. Call `shipstationClient.GetRates(ctx, req)` with origin=warehouse ZIP, dest=customer ZIP
4. Return top N rates sorted by price

#### [NEW] `internal/checkout/dto.go` addition
```go
type ShippingRateDTO struct {
    ServiceCode  string `json:"service_code"`
    Label        string `json:"label"`
    Cost         string `json:"cost"`
    Currency     string `json:"currency"`
    DeliveryDays *int   `json:"delivery_days,omitempty"`
}
```

#### [MODIFY] `internal/checkout/handler.go`
```go
// GET /checkout/shipping-rates?zip=90210&country=US
func (h *Handler) GetShippingRates(c *fiber.Ctx) error {
    uid, sid := h.extractAuth(c)
    zip     := c.Query("zip")
    country := c.Query("country", "US")

    rates, err := h.service.GetShippingRates(c.Context(), uid, sid, zip, country)
    if err != nil {
        return response.Error(c, err)
    }
    return response.Success(c, fiber.StatusOK, "Shipping rates retrieved", rates)
}
```

---

### Part D: Wire Up in main.go

#### [MODIFY] `cmd/server/main.go`
```go
shipstationClient := shipstation.NewClient(cfg.ShipStation.APIKey, cfg.ShipStation.Sandbox)
checkoutService := checkout.NewCheckoutService(cartService, shopifyClient, stockLockService, checkoutStore, cfg.App.FrontendURL, shipstationClient, cfg.ShipStation.WarehouseZip)
```

---

## Verification Plan

| Item | Test |
|---|---|
| Bug 1 (Order Excel) | `GET /admin/orders/export` → should download `.xlsx` not 500 |
| Bug 1 (Preorder Excel) | `GET /preorders/export` → should download `.xlsx` not 500 |
| Bug 2 (Sync) | Add product in Shopify → call `POST /products/sync` → new product appears in `GET /products` |
| ShipStation SDK | `GET /checkout/shipping-rates?zip=94602&country=US` → returns list of rates with costs |
| ShipStation Sandbox | Verify carrier ID by calling `ListCarriers` first and logging the response |
