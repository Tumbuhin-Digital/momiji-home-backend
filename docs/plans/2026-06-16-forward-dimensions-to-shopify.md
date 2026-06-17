# Plan: Forward Item Dimensions to Shopify Draft Order
*2026-06-16 — Shipping Accuracy for Pre-Order*

## Context

Unishippers calculates shipping rates using:
- ✅ Receiver ZIP code (already sent via `ShippingAddress` on draft order — Unishippers resolves residential/commercial from this)
- ✅ Weight (already sent via `DraftOrderLineItemWeightInput`)
- ❌ **Dimensions (L × W × H)** — stored in DB but never forwarded to Shopify

The dimensions are already in the DB on `product_variants`:
- `width_cm` (float64)
- `height_cm` (float64)
- `depth_cm` (float64)

But they get dropped when building the `CartItem` DTO, so they never reach the checkout service.

---

## Proposed Changes (3 files)

### 1. [MODIFY] `internal/cart/dto.go`

Add dimension fields to `CartItem`:

```go
type CartItem struct {
    // ...existing fields...
    Weight   float64 `json:"weight,omitempty"`    // kg — already exists
    WidthCm  float64 `json:"width_cm,omitempty"`  // ADD
    HeightCm float64 `json:"height_cm,omitempty"` // ADD
    DepthCm  float64 `json:"depth_cm,omitempty"`  // ADD
}
```

---

### 2. [MODIFY] `internal/cart/service.go`

In `GetCartResponse`, map the variant dimensions when building `CartItem`:

```go
cItem := CartItem{
    // ...existing fields...
    Weight:   variant.WeightKg,   // already there
    WidthCm:  variant.WidthCm,    // ADD
    HeightCm: variant.HeightCm,   // ADD
    DepthCm:  variant.DepthCm,    // ADD
}
```

---

### 3. [MODIFY] `internal/checkout/service.go`

In `InitiateCheckout`, forward dimensions as `customAttributes` on the pre-order draft line item.

Shopify's `DraftOrderLineItemInput` does not have a native dims field, so we attach them as custom attributes that Unishippers reads from the Shopify order:

```go
// After the existing weight block:
if item.WidthCm > 0 || item.HeightCm > 0 || item.DepthCm > 0 {
    draftLine.CustomAttributes = append(draftLine.CustomAttributes,
        shopify.AttributeInput{Key: "width_cm",  Value: fmt.Sprintf("%.2f", item.WidthCm)},
        shopify.AttributeInput{Key: "height_cm", Value: fmt.Sprintf("%.2f", item.HeightCm)},
        shopify.AttributeInput{Key: "depth_cm",  Value: fmt.Sprintf("%.2f", item.DepthCm)},
    )
}
```

> **Note on units:** The Unishippers invoice shows dimensions in inches (L: 25 W: 24 H: 12).
> Our DB stores in centimeters. One of two options:
> - Convert to inches before sending: `cm / 2.54`
> - Keep in cm and confirm with PO/Unishippers which unit they expect from Shopify attributes
> 
> **⚠️ Confirm unit with PO before implementing.**

---

## What We Do NOT Need to Change

- ❌ Residential vs. commercial detection → Unishippers resolves from ZIP automatically
- ❌ ZIP code sending → already in `ShippingAddress` on the draft order
- ❌ Weight → already forwarded correctly
