# Frontend Requested Changes Plan

> **Status:** 🔲 PENDING
> **Date:** 2026-06-08
> **Source:** Frontend Team Request

---

## 1. Integrate `image_url` and `inventory_quantity` in `GET /cart`

**Problem:** The Frontend's Cart Sheet needs to display product images and limit the quantity selector based on the maximum available stock.
**Current State:** `GET /cart` returns `image_src`, but not `inventory_quantity`.

### Proposed Changes

**File: `internal/cart/dto.go`**
- Update `CartItem` struct to include `InventoryQuantity`.
- Keep the `image_src` JSON tag as is.

```go
type CartItem struct {
    ID                string `json:"id"`
    VariantID         string `json:"variant_id"`
    Title             string `json:"title"`
    ImageSrc          string `json:"image_src"`           // Keep as image_src
    Quantity          int    `json:"quantity"`
    InventoryQuantity int    `json:"inventory_quantity"`  // NEW
    UnitPrice         string `json:"unit_price"`
    // ...
}
```

**File: `internal/cart/service.go`**
- In `GetCartResponse`, when building `cItem`, map the new fields from the `variant` (which is already being fetched via `GetVariantByID`).

```go
cItem := CartItem{
    ID:                item.ID,
    VariantID:         item.ShopifyVariantID,
    Title:             variant.Title,
    ImageSrc:          variant.ImageSrc,          
    Quantity:          item.Quantity,
    InventoryQuantity: variant.InventoryQuantity, // Map inventory
    // ...
}
```

---

## 2. Integrate `order_date`, `customer`, and `shipping_address` in `GET /order` response

**Problem:** The frontend needs to display when the order was placed, who placed it, and where it's shipping to.
**Decision:** Use the relational approach. The `OrderResponse` will include full nested `Customer` and `ShippingAddress` objects preloaded from the `customers` and `customer_addresses` tables via `customer_id` and `shipping_address_id`.

### Proposed Changes

**File: `internal/order/dto.go`**
- Define `CustomerDTO` and `AddressDTO` structs.
- Update `OrderResponse` to include `OrderDate`, `Customer`, and `ShippingAddress`.

```go
type CustomerDTO struct {
    ID        string `json:"id"`
    FirstName string `json:"first_name"`
    LastName  string `json:"last_name"`
    Email     string `json:"email"`
    Phone     string `json:"phone,omitempty"`
}

type AddressDTO struct {
    FirstName string `json:"first_name"`
    LastName  string `json:"last_name"`
    Address1  string `json:"address1"`
    Address2  string `json:"address2,omitempty"`
    City      string `json:"city"`
    Province  string `json:"province"`
    Country   string `json:"country"`
    Zip       string `json:"zip"`
    Phone     string `json:"phone,omitempty"`
}

type OrderResponse struct {
    ID                  string         `json:"id"`
    OrderNumber         string         `json:"order_number"`
    OrderDate           string         `json:"order_date"`      // NEW
    Customer            *CustomerDTO   `json:"customer"`        // NEW
    ShippingAddress     *AddressDTO    `json:"shipping_address"`// NEW
    // ...
}
```

**File: `internal/order/store.go`**
- Add GORM relationships to the `Order` struct if they aren't fully mapped yet.
```go
type Order struct {
    // ...
    Customer        *Customer        `gorm:"foreignKey:CustomerID"`
    ShippingAddress *CustomerAddress `gorm:"foreignKey:ShippingAddressID"`
}
```

**File: `internal/order/postgres.go`**
- Update the queries in `GetOrders` and `GetOrderByID` to include `.Preload("Customer").Preload("ShippingAddress")`.

**File: `internal/order/service.go`**
- In `mapOrderToDTO` (and wherever we construct `OrderResponse`), map the preloaded relations to the DTOs and set `OrderDate`.

```go
func mapOrderToDTO(order *Order) OrderResponse {
    dto := OrderResponse{
        // ...
        OrderDate: order.CreatedAt.Format(time.RFC3339),
    }

    if order.Customer != nil {
        dto.Customer = &CustomerDTO{
            ID:        order.Customer.ID,
            FirstName: order.Customer.FirstName,
            LastName:  order.Customer.LastName,
            Email:     order.Customer.Email,
            Phone:     order.Customer.Phone,
        }
    }

    if order.ShippingAddress != nil {
        dto.ShippingAddress = &AddressDTO{
            FirstName: order.ShippingAddress.FirstName,
            LastName:  order.ShippingAddress.LastName,
            Address1:  order.ShippingAddress.Address1,
            Address2:  order.ShippingAddress.Address2,
            City:      order.ShippingAddress.City,
            Province:  order.ShippingAddress.Province,
            Country:   order.ShippingAddress.Country,
            Zip:       order.ShippingAddress.Zip,
            Phone:     order.ShippingAddress.Phone,
        }
    }
    
    return dto
}
```

---

## 3. Prevent `ship_ready` status if inventory is 0

**Problem:** The admin shouldn't be allowed to mark a product as `ship_ready` if it has no physical stock, as that would allow immediate checkout without pre-order rules applying.
**Current State:** `PATCH /products/:id/status` blindly updates the DB without checking inventory.

### Proposed Changes

**File: `internal/product/service.go`**
- Add validation logic inside `UpdateProductStatus`.
- If the requested status is `ship_ready`, fetch all variants for the product.
- If **any** variant has `inventory_quantity <= 0`, block the update and return an error.

```go
func (s *service) UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) (*ProductDTO, error) {
    if fulfillmentType != "ship_ready" && fulfillmentType != "pre_order" {
        return nil, apierror.New(400, "validation_error", "fulfillment_type must be ship_ready or pre_order")
    }

    // NEW INVENTORY CHECK
    if fulfillmentType == "ship_ready" {
        variants, err := s.store.GetVariantsByProductID(ctx, productID)
        if err != nil {
            return nil, apierror.ErrInternal
        }
        for _, v := range variants {
            if v.InventoryQuantity <= 0 {
                return nil, apierror.New(400, "inventory_error", "Cannot set status to ship_ready. Variant '" + v.Title + "' has 0 inventory.")
            }
        }
    }

    if err := s.store.UpdateProductStatus(ctx, productID, fulfillmentType); err != nil {
        return nil, apierror.ErrInternal
    }
    // ...
}
```
