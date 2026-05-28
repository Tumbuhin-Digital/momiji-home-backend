# API Contract Alignment Plan

> **Status:** 🔲 PENDING
> **Scope:** Standardize API responses and query parameters across all modules to match the `api_contract/` markdown specifications.
> **Pre-requisite:** Database schema alignment (Phase 6 part 0) complete.

---

## Current State vs Contract

| Module | What's Built | What's Expected by Contract |
|--------|-------------|-----------------------------|
| `product/` | Flat array of variants in `data`, no pagination | `data` contains pagination metadata + nested `products` array. Products contain nested `variants` and `images` arrays. Query supports `page`, `limit`, `sort`, `search`, `fulfillment_type`. |
| `order/` | Flat array of orders in `data`, no pagination | `data` contains pagination metadata + `orders` array. Query supports `page`, `limit`, `status`, `search`. |
| `customer/` | Pagination meta outside `data` root | `data` contains pagination metadata + `customers` array. Query supports `page`, `limit`, `search`. |

---

## Proposed Changes (Helicopter View)

### 1. Shared Pagination Utility
- Implement a shared DTO structure for paginated responses (e.g., `PaginatedResponse`) containing `page`, `limit`, `total`, `totalPages`, and a generic data payload. This ensures all list endpoints follow the exact same envelope structure as defined in the contract.

### 2. Product Module Overhaul
- **DTOs:** Create `ProductQuery` struct for filtering. Update `ProductDTO` to nest `variants` and `images` according to the `Get Product List` contract.
- **Store:** Modify `GetVariants` to become a paginated `GetProducts` method. Use GORM's `Preload` to eagerly load variants alongside the products. Apply SQL filtering for search and status.
- **Service & Handler:** Parse query parameters, invoke the paginated store method, and wrap the resulting nested DTOs in the shared pagination envelope.

### 3. Order Module Alignment
- **DTOs:** Add `OrderQuery` struct. Update `OrderListResponse` to match the expected pagination structure.
- **Store:** Update the `GetOrders` method to accept pagination limits and filters (`status`, `search`).
- **Service & Handler:** Map the database results into the standard pagination envelope, ensuring the array is named `orders`.

### 4. Customer Module Alignment
- **Handler:** The current implementation places the `meta` object at the root of the response. Refactor the response serialization to place `page`, `limit`, and `total` inside the `data` object, matching the exact shape of the `Get Customer List` contract.

---

## Verification Plan

Once implemented, the following should be verified:
1. **Product List:** `GET /api/v1/products?page=1&limit=5&fulfillment_type=ship_ready` returns a `data` object with `totalPages` and a `products` array containing nested `variants`.
2. **Order List:** `GET /api/v1/orders` returns a `data` object with `orders` array and pagination metadata.
3. **Customer List:** `GET /api/v1/customers` returns a `data` object with `customers` array and pagination metadata.
