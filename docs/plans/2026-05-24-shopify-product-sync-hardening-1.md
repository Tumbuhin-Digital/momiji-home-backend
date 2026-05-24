# Phase 4.5 Plan: Shopify Product Sync — Hardening & Gap Closure

> **Status:** 🔲 PENDING
> **Scope:** Fix architectural leak, add pagination, enforce RBAC, add mock store, fill API contract gaps
> **Pre-requisite commits:** `4d12986`, `1707910`, `ae58053`, `2fa8161` (Phase 4 complete)
> **Source:** Implementation plan approved 2026-05-24 — `implementation_plan.md`

---

## Summary

Phase 4 scaffolded the product sync feature but left five defects:

| # | Defect | File |
|---|--------|------|
| 1 | Type assertion `s.store.(*PostgresStore)` leaks concrete type through interface | `internal/product/service.go:176` |
| 2 | GraphQL query fetches only first 50 products — no pagination | `internal/product/service.go:96` |
| 3 | Sync endpoint guarded by JWT but missing RBAC — any customer can trigger it | `internal/product/handler.go:27` |
| 4 | No `mock_store.go` — product service is untestable | missing |
| 5 | Five API contract endpoints are undeclared in handler | `internal/product/handler.go` |

---

## Gap Analysis

**Nouns:** `Product`, `ProductVariant`, `Store`, `MockStore`, `ShopifyClient`, `pageInfo`, `cursor`, `fulfillment_type`, `preorder_batch_label`, `ws_price`, `retail_price`, `status`
**Verbs:** `GetProductByShopifyID`, `GetProductByID`, `GetVariantsByProductID`, `UpdateProductStatus`, `UpdateVariantBatchLabel`, `UpdateVariantPrice`, `paginate`, `guard`, `mock`, `test`

All nouns/verbs mapped to Tasks 1–7 below. No exclusions.

---

## Tasks

---

### Task 1: Add `GetProductByShopifyID` to Store Interface

**Files:**
- Modify: `internal/product/store.go`

**Requirements:**

- **Acceptance Criteria**
  1. `Store` interface declares `GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error)`
  2. `go build ./...` passes with no type mismatch errors

- **Functional Requirements**
  1. Method signature must match GORM nil-on-not-found pattern used by `GetVariantByShopifyID` in `postgres.go`

- **Non-Functional Requirements**
  None for this task.

- **Test Coverage**
  - [Unit] Covered by Task 4's `MockProductStore` — method must be present or mock will not compile
  - Test data fixtures: none (compile-time check)

**Step 1: Write failing test**
```go
// internal/product/store_contract_test.go
package product_test

import "testing"

// Compile-time interface assertion — fails if method is missing from Store
var _ Store = (*MockProductStore)(nil)

func TestStoreInterface(t *testing.T) {
    // This test file verifies the Store interface is complete.
    // Actual behaviour tested in Task 4 service_test.go.
    t.Log("Store interface compile check passed")
}
```

**Step 2: Verify test fails**
```
Run: go test ./internal/product/... -run TestStoreInterface -v
Expected: FAIL — cannot use MockProductStore (type *MockProductStore) as type Store: missing GetProductByShopifyID method
```

**Step 3: Write minimal implementation**
```go
// internal/product/store.go — add to Store interface
GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error)
```

**Step 4: Verify test passes**
```
Run: go test ./internal/product/... -run TestStoreInterface -v
Expected: PASS (after Task 2 adds postgres impl and Task 4 adds mock impl)
```

---

### Task 2: Implement `GetProductByShopifyID` on `PostgresStore`

**Files:**
- Modify: `internal/product/postgres.go`

**Requirements:**

- **Acceptance Criteria**
  1. `PostgresStore.GetProductByShopifyID` returns `(*Product, nil)` when a product with the given `shopify_id` exists
  2. Returns `(nil, nil)` when not found (matches `GetVariantByShopifyID` and `GetUserByID` nil-on-not-found contract)
  3. Returns `(nil, err)` on DB error

- **Functional Requirements**
  1. Query: `WHERE shopify_id = ?` on the `products` table via GORM
  2. Wrap `gorm.ErrRecordNotFound` → `return nil, nil` — same pattern as `internal/auth/postgres.go:GetUserByID`

- **Non-Functional Requirements**
  None for this task.

- **Test Coverage**
  - [Integration] `PostgresStore.GetProductByShopifyID` — not-found returns nil,nil; found returns record; DB error propagates
  - Test data fixtures: seed one `Product` row via `store.UpsertProduct`

**Step 1: Write failing test**
```go
// internal/product/postgres_integration_test.go
//go:build integration

package product_test

import (
    "context"
    "testing"
)

func TestPostgresStore_GetProductByShopifyID(t *testing.T) {
    store := setupTestDB(t) // helper that connects to test DB and returns Store

    ctx := context.Background()

    t.Run("not found returns nil", func(t *testing.T) {
        p, err := store.GetProductByShopifyID(ctx, "nonexistent")
        if err != nil { t.Fatalf("unexpected error: %v", err) }
        if p != nil { t.Fatal("expected nil product") }
    })

    t.Run("found returns product", func(t *testing.T) {
        _ = store.UpsertProduct(ctx, &Product{ShopifyID: "gid://shopify/Product/123", Title: "Test", Status: "active"})
        p, err := store.GetProductByShopifyID(ctx, "gid://shopify/Product/123")
        if err != nil { t.Fatalf("unexpected error: %v", err) }
        if p == nil { t.Fatal("expected product, got nil") }
        if p.Title != "Test" { t.Fatalf("expected title Test, got %s", p.Title) }
    })
}
```

**Step 2: Verify test fails**
```
Run: go test -tags integration ./internal/product/... -run TestPostgresStore_GetProductByShopifyID -v
Expected: FAIL — undefined: GetProductByShopifyID or method not found
```

**Step 3: Write minimal implementation**
```go
// internal/product/postgres.go — add method to PostgresStore
func (s *PostgresStore) GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error) {
    var product Product
    err := s.db.WithContext(ctx).Where("shopify_id = ?", shopifyID).First(&product).Error
    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return nil, nil
        }
        return nil, err
    }
    return &product, nil
}
```

**Step 4: Verify test passes**
```
Run: go test -tags integration ./internal/product/... -run TestPostgresStore_GetProductByShopifyID -v
Expected: PASS with exit code 0
```

---

### Task 3: Fix Architectural Leak in `SyncFromShopify`

**Files:**
- Modify: `internal/product/service.go:173-195`

**Requirements:**

- **Acceptance Criteria**
  1. `service.go` contains zero type assertions (grep `(*PostgresStore)` returns no results in `service.go`)
  2. `SyncFromShopify` calls `s.store.GetProductByShopifyID` to retrieve the persisted product UUID
  3. `go build ./...` passes
  4. `go vet ./internal/product/...` passes

- **Functional Requirements**
  1. After calling `s.store.UpsertProduct(ctx, product)`, call `s.store.GetProductByShopifyID(ctx, product.ShopifyID)` to obtain the DB-assigned UUID
  2. If `GetProductByShopifyID` returns `nil, nil` (not found after upsert), log a warning via `slog.WarnContext` and skip variant upsert for this product — do not abort the entire sync
  3. If `GetProductByShopifyID` returns an error, return it wrapped: `fmt.Errorf("failed to reload product %s: %w", shopifyID, err)`

- **Non-Functional Requirements**
  - No direct GORM or DB imports inside `service.go` — service layer must remain DB-agnostic

- **Test Coverage**
  - [Unit] `service.SyncFromShopify` — verify `GetProductByShopifyID` is called once per product (covered by Task 4 mock)
  - Test data fixtures: MockShopifyClient returns a single product with one variant

**Step 1: Write failing test**
```go
// internal/product/service_test.go (skeleton, full body in Task 4)
package product_test

import (
    "context"
    "testing"
)

func TestSyncFromShopify_UsesStoreNotTypeAssertion(t *testing.T) {
    mockStore := NewMockProductStore()
    mockClient := NewMockShopifyClient()
    svc := NewProductService(mockStore, mockClient)

    err := svc.SyncFromShopify(context.Background())
    if err != nil { t.Fatalf("unexpected error: %v", err) }

    // Verify GetProductByShopifyID was called (mock tracks calls)
    if mockStore.GetProductByShopifyIDCalls == 0 {
        t.Fatal("expected GetProductByShopifyID to be called, got 0 calls")
    }
}
```

**Step 2: Verify test fails**
```
Run: go test ./internal/product/... -run TestSyncFromShopify_UsesStoreNotTypeAssertion -v
Expected: FAIL — MockProductStore undefined (mock not yet created) OR GetProductByShopifyIDCalls == 0 because old code uses type assertion
```

**Step 3: Write minimal implementation**
```go
// internal/product/service.go — replace lines 173-195
// Replace:
//   if dbErr := s.store.(*PostgresStore).db.Where(...).First(&p).Error; dbErr == nil {
// With:

p, err := s.store.GetProductByShopifyID(ctx, product.ShopifyID)
if err != nil {
    return fmt.Errorf("failed to reload product %s: %w", product.ShopifyID, err)
}
if p == nil {
    slog.WarnContext(ctx, "product not found after upsert, skipping variants",
        slog.String("shopify_id", product.ShopifyID))
    continue
}
for _, vEdge := range pNode.Variants.Edges {
    // ... rest of variant upsert unchanged, replace p.ID with p.ID
}
```

**Step 4: Verify test passes**
```
Run: go test ./internal/product/... -run TestSyncFromShopify_UsesStoreNotTypeAssertion -v
Expected: PASS with exit code 0
```

---

### Task 4: Add `MockProductStore` and `MockShopifyClient`

**Files:**
- Create: `internal/product/mock_store.go`

**Requirements:**

- **Acceptance Criteria**
  1. `MockProductStore` implements all methods of `Store` interface (compile-time: `var _ Store = (*MockProductStore)(nil)`)
  2. `MockShopifyClient` implements `shopify.Client` interface
  3. Each mock method records call counts and accepts injectable return values via exported fields
  4. `go build ./...` passes

- **Functional Requirements**
  1. `MockProductStore` uses in-memory `map[string]*ProductVariant` keyed by `ShopifyVariantID` and `map[string]*Product` keyed by `ShopifyID` — mirrors `MockAuthStore` pattern from `internal/auth/mock_store.go`
  2. `MockShopifyClient.QueryAdminGraphQL` returns a configurable `[]byte` response and `error` — set via `MockShopifyClient.AdminGraphQLResponse` and `MockShopifyClient.AdminGraphQLErr` fields
  3. Call counters: `GetProductByShopifyIDCalls int`, `UpsertProductCalls int`, `UpsertVariantCalls int`

- **Non-Functional Requirements**
  None for this task.

- **Test Coverage**
  - [Unit] `var _ Store = (*MockProductStore)(nil)` — compile-time interface assertion
  - [Unit] `var _ shopify.Client = (*MockShopifyClient)(nil)` — compile-time interface assertion

**Step 1: Write failing test**
```go
// internal/product/mock_store_test.go
package product_test

import "testing"

func TestMockProductStore_ImplementsStore(t *testing.T) {
    var _ Store = (*MockProductStore)(nil)
}

func TestMockShopifyClient_ImplementsClient(t *testing.T) {
    var _ shopifyClient = (*MockShopifyClient)(nil)
}
```

**Step 2: Verify test fails**
```
Run: go test ./internal/product/... -run TestMockProductStore_ImplementsStore -v
Expected: FAIL — undefined: MockProductStore
```

**Step 3: Write minimal implementation**
```go
// internal/product/mock_store.go
package product

import (
    "context"
    "errors"

    "github.com/tumbuhindigi-sys/momiji-home-backend/internal/platform/shopify"
)

// MockProductStore is an in-memory Store for unit tests.
type MockProductStore struct {
    Products  map[string]*Product        // keyed by ShopifyID
    Variants  map[string]*ProductVariant // keyed by ShopifyVariantID
    VariantsByID map[string]*ProductVariant // keyed by UUID ID

    GetProductByShopifyIDCalls int
    UpsertProductCalls         int
    UpsertVariantCalls         int

    // Injectable errors
    GetVariantsErr             error
    GetVariantByShopifyIDErr   error
    GetProductByShopifyIDErr   error
    UpsertProductErr           error
    UpsertVariantErr           error
    UpdateVariantPricesErr     error
    GetProductByIDErr          error
    GetVariantsByProductIDErr  error
    UpdateProductStatusErr     error
    UpdateVariantBatchLabelErr error
}

func NewMockProductStore() *MockProductStore {
    return &MockProductStore{
        Products:     make(map[string]*Product),
        Variants:     make(map[string]*ProductVariant),
        VariantsByID: make(map[string]*ProductVariant),
    }
}

func (m *MockProductStore) GetVariants(ctx context.Context) ([]ProductVariant, error) {
    if m.GetVariantsErr != nil { return nil, m.GetVariantsErr }
    out := make([]ProductVariant, 0, len(m.Variants))
    for _, v := range m.Variants { out = append(out, *v) }
    return out, nil
}

func (m *MockProductStore) GetVariantByShopifyID(ctx context.Context, id string) (*ProductVariant, error) {
    if m.GetVariantByShopifyIDErr != nil { return nil, m.GetVariantByShopifyIDErr }
    v, ok := m.Variants[id]
    if !ok { return nil, nil }
    return v, nil
}

func (m *MockProductStore) GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error) {
    m.GetProductByShopifyIDCalls++
    if m.GetProductByShopifyIDErr != nil { return nil, m.GetProductByShopifyIDErr }
    p, ok := m.Products[shopifyID]
    if !ok { return nil, nil }
    return p, nil
}

func (m *MockProductStore) UpsertProduct(ctx context.Context, product *Product) error {
    m.UpsertProductCalls++
    if m.UpsertProductErr != nil { return m.UpsertProductErr }
    if product.ID == "" { product.ID = "mock-product-uuid" }
    m.Products[product.ShopifyID] = product
    return nil
}

func (m *MockProductStore) UpsertVariant(ctx context.Context, variant *ProductVariant) error {
    m.UpsertVariantCalls++
    if m.UpsertVariantErr != nil { return m.UpsertVariantErr }
    if variant.ID == "" { variant.ID = "mock-variant-uuid" }
    m.Variants[variant.ShopifyVariantID] = variant
    m.VariantsByID[variant.ID] = variant
    return nil
}

func (m *MockProductStore) UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64, retailPrice *float64) error {
    if m.UpdateVariantPricesErr != nil { return m.UpdateVariantPricesErr }
    v, ok := m.VariantsByID[variantID]
    if !ok { return errors.New("variant not found") }
    v.WSPrice = wsPrice
    v.RetailPrice = retailPrice
    return nil
}

func (m *MockProductStore) GetProductByID(ctx context.Context, productID string) (*Product, error) {
    if m.GetProductByIDErr != nil { return nil, m.GetProductByIDErr }
    for _, p := range m.Products {
        if p.ID == productID { return p, nil }
    }
    return nil, nil
}

func (m *MockProductStore) GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error) {
    if m.GetVariantsByProductIDErr != nil { return nil, m.GetVariantsByProductIDErr }
    out := []ProductVariant{}
    for _, v := range m.Variants {
        if v.ProductID == productID { out = append(out, *v) }
    }
    return out, nil
}

func (m *MockProductStore) UpdateProductStatus(ctx context.Context, productID string, status string) error {
    if m.UpdateProductStatusErr != nil { return m.UpdateProductStatusErr }
    for _, p := range m.Products {
        if p.ID == productID { p.Status = status; return nil }
    }
    return errors.New("product not found")
}

func (m *MockProductStore) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string) error {
    if m.UpdateVariantBatchLabelErr != nil { return m.UpdateVariantBatchLabelErr }
    for _, v := range m.Variants {
        if v.ProductID == productID { v.PreorderBatchLabel = &batchLabel }
    }
    return nil
}

// MockShopifyClient is a test double for shopify.Client.
type MockShopifyClient struct {
    AdminGraphQLResponse []byte
    AdminGraphQLErr      error
    DraftOrderResponse   *shopify.DraftOrderResponse
    DraftOrderErr        error
    CheckoutResponse     *shopify.CheckoutResponse
    CheckoutErr          error
}

func (m *MockShopifyClient) QueryAdminGraphQL(ctx context.Context, query string, variables map[string]interface{}) ([]byte, error) {
    return m.AdminGraphQLResponse, m.AdminGraphQLErr
}

func (m *MockShopifyClient) CreateDraftOrder(ctx context.Context, input shopify.DraftOrderInput) (*shopify.DraftOrderResponse, error) {
    return m.DraftOrderResponse, m.DraftOrderErr
}

func (m *MockShopifyClient) CreateStorefrontCheckout(ctx context.Context, input shopify.CheckoutCreateInput) (*shopify.CheckoutResponse, error) {
    return m.CheckoutResponse, m.CheckoutErr
}
```

**Step 4: Verify test passes**
```
Run: go test ./internal/product/... -run TestMockProductStore_ImplementsStore -v
Expected: PASS with exit code 0
```

---

### Task 5: Write Unit Tests for `ProductService`

**Files:**
- Create: `internal/product/service_test.go`

**Requirements:**

- **Acceptance Criteria**
  1. All tests pass with `go test ./internal/product/... -v`
  2. No external infrastructure required (pure in-memory mock)
  3. `SyncFromShopify` test verifies products and variants are upserted via mock store

- **Functional Requirements**
  1. `TestGetVariantByID_Found` — seed variant in mock, call service, verify DTO fields match
  2. `TestGetVariantByID_NotFound` — empty mock, verify `apierror.ErrNotFound` returned
  3. `TestGetVariants_Empty` — verify empty slice returned (not nil)
  4. `TestSyncFromShopify_Success` — MockShopifyClient returns valid single-page JSON, verify `UpsertProductCalls == 1` and `UpsertVariantCalls == 1`
  5. `TestSyncFromShopify_ClientError` — MockShopifyClient returns error, verify sync returns wrapped error

- **Non-Functional Requirements**
  - Each test must complete in < 100ms

- **Test Coverage**
  - [Unit] `GetVariantByID` — found path, not-found path, store error path
  - [Unit] `SyncFromShopify` — success, client error, store upsert error

**Step 1: Write failing test**
```go
// internal/product/service_test.go
package product_test

import (
    "context"
    "encoding/json"
    "testing"

    "github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
    "github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

func buildShopifyProductResponse(shopifyProductID, shopifyVariantID, title, price string) []byte {
    resp := map[string]interface{}{
        "data": map[string]interface{}{
            "products": map[string]interface{}{
                "edges": []map[string]interface{}{
                    {
                        "node": map[string]interface{}{
                            "id": shopifyProductID, "title": title,
                            "descriptionHtml": "", "status": "ACTIVE",
                            "variants": map[string]interface{}{
                                "edges": []map[string]interface{}{
                                    {"node": map[string]interface{}{
                                        "id": shopifyVariantID, "title": title,
                                        "sku": "SKU-001", "price": price,
                                        "image": map[string]string{"url": ""},
                                    }},
                                },
                            },
                            "pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
                        },
                    },
                },
                "pageInfo": map[string]interface{}{"hasNextPage": false, "endCursor": ""},
            },
        },
    }
    b, _ := json.Marshal(resp)
    return b
}

func TestGetVariantByID_Found(t *testing.T) {
    store := product.NewMockProductStore()
    svc := product.NewProductService(store, &product.MockShopifyClient{})

    store.Variants["gid://shopify/ProductVariant/1"] = &product.ProductVariant{
        ID: "uuid-1", ShopifyVariantID: "gid://shopify/ProductVariant/1",
        Title: "Test Variant", Price: 100.00, FulfillmentType: "ship_ready",
    }

    dto, err := svc.GetVariantByID(context.Background(), "gid://shopify/ProductVariant/1")
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    if dto.Title != "Test Variant" { t.Fatalf("expected 'Test Variant', got '%s'", dto.Title) }
}

func TestGetVariantByID_NotFound(t *testing.T) {
    store := product.NewMockProductStore()
    svc := product.NewProductService(store, &product.MockShopifyClient{})

    _, err := svc.GetVariantByID(context.Background(), "nonexistent")
    if err != apierror.ErrNotFound { t.Fatalf("expected ErrNotFound, got %v", err) }
}

func TestGetVariants_Empty(t *testing.T) {
    store := product.NewMockProductStore()
    svc := product.NewProductService(store, &product.MockShopifyClient{})

    variants, err := svc.GetVariants(context.Background())
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    if variants == nil { t.Fatal("expected empty slice, got nil") }
    if len(variants) != 0 { t.Fatalf("expected 0 variants, got %d", len(variants)) }
}

func TestSyncFromShopify_Success(t *testing.T) {
    store := product.NewMockProductStore()
    mockClient := &product.MockShopifyClient{
        AdminGraphQLResponse: buildShopifyProductResponse(
            "gid://shopify/Product/1", "gid://shopify/ProductVariant/1", "Rose Mug", "45.00",
        ),
    }
    svc := product.NewProductService(store, mockClient)

    err := svc.SyncFromShopify(context.Background())
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    if store.UpsertProductCalls != 1 { t.Fatalf("expected 1 UpsertProduct call, got %d", store.UpsertProductCalls) }
    if store.UpsertVariantCalls != 1 { t.Fatalf("expected 1 UpsertVariant call, got %d", store.UpsertVariantCalls) }
}

func TestSyncFromShopify_ClientError(t *testing.T) {
    store := product.NewMockProductStore()
    mockClient := &product.MockShopifyClient{AdminGraphQLErr: errors.New("shopify timeout")}
    svc := product.NewProductService(store, mockClient)

    err := svc.SyncFromShopify(context.Background())
    if err == nil { t.Fatal("expected error, got nil") }
}
```

**Step 2: Verify test fails**
```
Run: go test ./internal/product/... -run "TestGetVariantByID|TestGetVariants|TestSyncFromShopify" -v
Expected: FAIL — MockShopifyClient fields undefined until Task 4 is complete, or logic fails
```

**Step 3: Write minimal implementation**
- No new implementation code — tests are complete. Tasks 1–4 provide all implementation.

**Step 4: Verify test passes**
```
Run: go test ./internal/product/... -run "TestGetVariantByID|TestGetVariants|TestSyncFromShopify" -v
Expected: PASS all 5 tests with exit code 0
```

---

### Task 6: Implement Paginated Sync in `SyncFromShopify`

**Files:**
- Modify: `internal/product/service.go` — `SyncFromShopify` method body

**Requirements:**

- **Acceptance Criteria**
  1. `SyncFromShopify` loops until `pageInfo.hasNextPage == false` or safety cap of 10 pages is reached
  2. Each loop passes `after: $cursor` variable via `QueryAdminGraphQL` variables map
  3. On cap reached, logs `slog.WarnContext` with message `"shopify sync page cap reached, some products may be missing"`
  4. `go test ./internal/product/... -run TestSyncFromShopify` passes

- **Functional Requirements**
  1. GraphQL query must declare `$cursor: String` variable and pass `products(first: 50, after: $cursor)`
  2. Response struct must include `pageInfo { hasNextPage endCursor }` at the `products` level
  3. `cursor` variable starts as `nil` (first page), then is set to `endCursor` on each iteration
  4. Max pages constant: `const shopifySyncPageCap = 10`

- **Non-Functional Requirements**
  - Total sync for 500 products (10 pages × 50) must complete without timeout at default `http.Client` timeout

- **Test Coverage**
  - [Unit] `TestSyncFromShopify_MultiPage` — MockShopifyClient returns `hasNextPage: true` on first call, `false` on second; verify `UpsertProductCalls == 2`

**Step 1: Write failing test**
```go
// internal/product/service_test.go — add test
func TestSyncFromShopify_MultiPage(t *testing.T) {
    store := product.NewMockProductStore()
    callCount := 0
    responses := [][]byte{
        buildShopifyProductResponseWithPageInfo(
            "gid://shopify/Product/1", "gid://shopify/ProductVariant/1",
            true, "cursor-abc",
        ),
        buildShopifyProductResponseWithPageInfo(
            "gid://shopify/Product/2", "gid://shopify/ProductVariant/2",
            false, "",
        ),
    }
    mockClient := &product.MockShopifyClientFunc{
        QueryAdminGraphQLFn: func(ctx context.Context, query string, vars map[string]interface{}) ([]byte, error) {
            resp := responses[callCount]
            callCount++
            return resp, nil
        },
    }
    svc := product.NewProductService(store, mockClient)

    err := svc.SyncFromShopify(context.Background())
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    if store.UpsertProductCalls != 2 { t.Fatalf("expected 2 UpsertProduct calls, got %d", store.UpsertProductCalls) }
}
```

**Step 2: Verify test fails**
```
Run: go test ./internal/product/... -run TestSyncFromShopify_MultiPage -v
Expected: FAIL — UpsertProductCalls == 1, pagination loop not implemented
```

**Step 3: Write minimal implementation**
```go
// internal/product/service.go — rewrite SyncFromShopify
const shopifySyncPageCap = 10

func (s *service) SyncFromShopify(ctx context.Context) error {
    query := `
        query($cursor: String) {
          products(first: 50, after: $cursor) {
            pageInfo { hasNextPage endCursor }
            edges {
              node {
                id title descriptionHtml status
                variants(first: 10) {
                  edges {
                    node { id title sku price image { url } }
                  }
                }
              }
            }
          }
        }
    `

    var cursor *string
    for page := 0; page < shopifySyncPageCap; page++ {
        vars := map[string]interface{}{"cursor": cursor}

        resBytes, err := s.client.QueryAdminGraphQL(ctx, query, vars)
        if err != nil {
            return fmt.Errorf("shopify graphql query failed (page %d): %w", page+1, err)
        }

        var res struct {
            Data struct {
                Products struct {
                    PageInfo struct {
                        HasNextPage bool   `json:"hasNextPage"`
                        EndCursor   string `json:"endCursor"`
                    } `json:"pageInfo"`
                    Edges []struct {
                        Node struct {
                            ID              string `json:"id"`
                            Title           string `json:"title"`
                            DescriptionHtml string `json:"descriptionHtml"`
                            Status          string `json:"status"`
                            Variants        struct {
                                Edges []struct {
                                    Node struct {
                                        ID    string `json:"id"`
                                        Title string `json:"title"`
                                        Sku   string `json:"sku"`
                                        Price string `json:"price"`
                                        Image struct{ Url string `json:"url"` } `json:"image"`
                                    } `json:"node"`
                                } `json:"edges"`
                            } `json:"variants"`
                        } `json:"node"`
                    } `json:"edges"`
                } `json:"products"`
            } `json:"data"`
        }

        if err := json.Unmarshal(resBytes, &res); err != nil {
            return fmt.Errorf("failed to parse shopify response (page %d): %w", page+1, err)
        }

        for _, pEdge := range res.Data.Products.Edges {
            pNode := pEdge.Node
            product := &Product{
                ShopifyID:   pNode.ID,
                Title:       pNode.Title,
                Description: pNode.DescriptionHtml,
                Status:      pNode.Status,
            }
            if err := s.store.UpsertProduct(ctx, product); err != nil {
                return fmt.Errorf("failed to upsert product %s: %w", product.ShopifyID, err)
            }

            p, err := s.store.GetProductByShopifyID(ctx, product.ShopifyID)
            if err != nil {
                return fmt.Errorf("failed to reload product %s: %w", product.ShopifyID, err)
            }
            if p == nil {
                slog.WarnContext(ctx, "product not found after upsert, skipping variants",
                    slog.String("shopify_id", product.ShopifyID))
                continue
            }

            for _, vEdge := range pNode.Variants.Edges {
                vNode := vEdge.Node
                price, _ := strconv.ParseFloat(vNode.Price, 64)
                variant := &ProductVariant{
                    ProductID:        p.ID,
                    ShopifyVariantID: vNode.ID,
                    Title:            vNode.Title,
                    SKU:              vNode.Sku,
                    Price:            price,
                    ImageSrc:         vNode.Image.Url,
                    FulfillmentType:  string(FulfillmentTypeShipReady),
                }
                if err := s.store.UpsertVariant(ctx, variant); err != nil {
                    return fmt.Errorf("failed to upsert variant %s: %w", variant.ShopifyVariantID, err)
                }
            }
        }

        if !res.Data.Products.PageInfo.HasNextPage {
            break
        }
        endCursor := res.Data.Products.PageInfo.EndCursor
        cursor = &endCursor

        if page == shopifySyncPageCap-1 {
            slog.WarnContext(ctx, "shopify sync page cap reached, some products may be missing",
                slog.Int("cap", shopifySyncPageCap))
        }
    }

    return nil
}
```

**Step 4: Verify test passes**
```
Run: go test ./internal/product/... -run TestSyncFromShopify -v
Expected: PASS all sync tests with exit code 0
```

---

### Task 7: Enforce RBAC on Sync Endpoint

**Files:**
- Modify: `internal/product/handler.go:24-29`

**Requirements:**

- **Acceptance Criteria**
  1. `POST /api/v1/products/sync` returns `403 Forbidden` when called with a valid JWT containing `role: customer`
  2. `POST /api/v1/products/sync` returns `200 OK` when called with a valid JWT containing `role: admin`
  3. `grep -n "RBAC" internal/product/handler.go` returns at least one match
  4. The `// TODO: Add admin role check middleware` comment is removed

- **Functional Requirements**
  1. Add `middleware.RBAC("admin")` to the admin sub-group in `SetupRoutes`, after `middleware.Auth(h.jwtSecret)`
  2. Follow exact pattern from `internal/auth/handler.go:72-74` (protected sub-group + Use)

- **Non-Functional Requirements**
  - RBAC check executes before handler function — middleware chain order: `Auth → RBAC → Handler`

- **Test Coverage**
  - [Unit] Handler route test: call `/products/sync` with `role=customer` fiber context → expect 403
  - [Unit] Handler route test: call `/products/sync` with `role=admin` fiber context → expect 200

**Step 1: Write failing test**
```go
// internal/product/handler_test.go
package product_test

import (
    "io"
    "net/http/httptest"
    "testing"

    "github.com/gofiber/fiber/v2"
    "github.com/tumbuhindigi-sys/momiji-home-backend/internal/product"
)

func TestSyncEndpoint_CustomerRoleReturns403(t *testing.T) {
    app := fiber.New()
    store := product.NewMockProductStore()
    svc := product.NewProductService(store, &product.MockShopifyClient{})
    h := product.NewProductHandler(svc, "test-secret")

    // Mount handler
    api := app.Group("/api/v1")
    h.SetupRoutes(api)

    // Simulate a customer-role JWT (middleware.Auth sets Locals, we set manually for unit test)
    app.Use(func(c *fiber.Ctx) error {
        c.Locals("user_id", "test-user")
        c.Locals("role", "customer") // Not admin
        return c.Next()
    })

    req := httptest.NewRequest("POST", "/api/v1/products/sync", nil)
    resp, _ := app.Test(req)
    defer resp.Body.Close()

    if resp.StatusCode != 403 {
        body, _ := io.ReadAll(resp.Body)
        t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
    }
}
```

**Step 2: Verify test fails**
```
Run: go test ./internal/product/... -run TestSyncEndpoint_CustomerRoleReturns403 -v
Expected: FAIL — got 200 instead of 403 (RBAC not yet enforced)
```

**Step 3: Write minimal implementation**
```go
// internal/product/handler.go — replace SetupRoutes admin block
func (h *Handler) SetupRoutes(router fiber.Router) {
    group := router.Group("/products")

    // Public
    group.Get("/", h.GetProducts)

    // Admin — requires valid JWT and admin role
    admin := group.Group("/")
    admin.Use(middleware.Auth(h.jwtSecret))
    admin.Use(middleware.RBAC("admin")) // replaces TODO comment
    admin.Post("/sync", h.SyncProducts)
}
```

**Step 4: Verify test passes**
```
Run: go test ./internal/product/... -run TestSyncEndpoint_CustomerRoleReturns403 -v
Expected: PASS with exit code 0
```

---

### Task 8: Add Missing Store Methods for API Contract Endpoints

**Files:**
- Modify: `internal/product/store.go`
- Modify: `internal/product/postgres.go`

**Requirements:**

- **Acceptance Criteria**
  1. `Store` interface declares all four new methods
  2. `PostgresStore` implements all four new methods
  3. `MockProductStore` (Task 4) already implements all four — compile-time assertion passes
  4. `go build ./...` passes

- **Functional Requirements**
  1. `GetProductByID(ctx, id string) (*Product, error)` — query `products.id = ?`, nil-on-not-found pattern
  2. `GetVariantsByProductID(ctx, productID string) ([]ProductVariant, error)` — query `product_variants.product_id = ?`
  3. `UpdateProductStatus(ctx, productID, status string) error` — `UPDATE products SET status=?, updated_at=now() WHERE id=?`
  4. `UpdateVariantBatchLabel(ctx, productID, batchLabel string) error` — `UPDATE product_variants SET preorder_batch_label=?, updated_at=now() WHERE product_id=?`

- **Non-Functional Requirements**
  - All GORM queries must use parameterized form (no raw SQL string interpolation)

- **Test Coverage**
  - [Integration] `TestPostgresStore_GetProductByID` — found and not-found cases
  - [Integration] `TestPostgresStore_GetVariantsByProductID` — returns all variants for product

**Step 1: Write failing test**
```go
// internal/product/postgres_integration_test.go — add cases
//go:build integration

func TestPostgresStore_GetProductByID(t *testing.T) {
    store := setupTestDB(t)
    ctx := context.Background()

    _ = store.UpsertProduct(ctx, &Product{ShopifyID: "gid://shopify/Product/42", Title: "Mug", Status: "active"})
    p, _ := store.GetProductByShopifyID(ctx, "gid://shopify/Product/42")

    found, err := store.GetProductByID(ctx, p.ID)
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    if found == nil { t.Fatal("expected product, got nil") }
    if found.Title != "Mug" { t.Fatalf("expected Mug, got %s", found.Title) }

    notFound, err := store.GetProductByID(ctx, "00000000-0000-0000-0000-000000000000")
    if err != nil { t.Fatalf("unexpected error: %v", err) }
    if notFound != nil { t.Fatal("expected nil for unknown ID") }
}
```

**Step 2: Verify test fails**
```
Run: go test -tags integration ./internal/product/... -run TestPostgresStore_GetProductByID -v
Expected: FAIL — undefined: GetProductByID
```

**Step 3: Write minimal implementation**
```go
// internal/product/store.go — add to Store interface
GetProductByID(ctx context.Context, productID string) (*Product, error)
GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error)
UpdateProductStatus(ctx context.Context, productID string, status string) error
UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string) error

// internal/product/postgres.go — add implementations
func (s *PostgresStore) GetProductByID(ctx context.Context, productID string) (*Product, error) {
    var p Product
    err := s.db.WithContext(ctx).Where("id = ?", productID).First(&p).Error
    if err != nil {
        if errors.Is(err, gorm.ErrRecordNotFound) { return nil, nil }
        return nil, err
    }
    return &p, nil
}

func (s *PostgresStore) GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error) {
    var variants []ProductVariant
    err := s.db.WithContext(ctx).Where("product_id = ?", productID).Find(&variants).Error
    return variants, err
}

func (s *PostgresStore) UpdateProductStatus(ctx context.Context, productID string, status string) error {
    return s.db.WithContext(ctx).Model(&Product{}).
        Where("id = ?", productID).
        Updates(map[string]interface{}{"status": status, "updated_at": gorm.Expr("now()")}).Error
}

func (s *PostgresStore) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string) error {
    return s.db.WithContext(ctx).Model(&ProductVariant{}).
        Where("product_id = ?", productID).
        Updates(map[string]interface{}{"preorder_batch_label": batchLabel, "updated_at": gorm.Expr("now()")}).Error
}
```

**Step 4: Verify test passes**
```
Run: go test -tags integration ./internal/product/... -run TestPostgresStore_GetProductByID -v
Expected: PASS with exit code 0
```

---

### Task 9: Add Missing Service Methods

**Files:**
- Modify: `internal/product/service.go`

**Requirements:**

- **Acceptance Criteria**
  1. `ProductService` interface declares all five new methods
  2. `service` struct implements all five
  3. `go build ./...` passes

- **Functional Requirements**
  1. `GetProductByID(ctx, id string) (*ProductDetailDTO, error)` — calls `store.GetProductByID`, maps to new `ProductDetailDTO` struct; returns `apierror.ErrNotFound` if nil
  2. `GetVariantsByProductID(ctx, productID string) ([]VariantDTO, error)` — calls `store.GetVariantsByProductID`, maps via existing `mapVariantToDTO`
  3. `UpdateProductStatus(ctx, productID, status string) error` — validates `status` is one of `"active"`, `"draft"`, `"archived"`; calls `store.UpdateProductStatus`
  4. `UpdateVariantBatchLabel(ctx, productID, batchLabel string) error` — calls `store.UpdateVariantBatchLabel`
  5. `UpdateVariantPrice(ctx, variantID string, wsPrice, retailPrice *float64) error` — calls `store.UpdateVariantPrices`; returns `apierror.ErrBadRequest` if both args are nil

- **Non-Functional Requirements**
  - `ProductDetailDTO` is a new struct: `{ ID, ShopifyID, Title, Description, Status string }` — added in `service.go` alongside `VariantDTO`

- **Test Coverage**
  - [Unit] `TestGetProductByID_Found`, `TestGetProductByID_NotFound`
  - [Unit] `TestUpdateProductStatus_InvalidStatus` — verify bad status returns error

**Step 1: Write failing test**
```go
// internal/product/service_test.go — add tests
func TestGetProductByID_NotFound(t *testing.T) {
    store := product.NewMockProductStore()
    svc := product.NewProductService(store, &product.MockShopifyClient{})

    _, err := svc.GetProductByID(context.Background(), "nonexistent-uuid")
    if err == nil { t.Fatal("expected error, got nil") }
}

func TestUpdateProductStatus_InvalidStatus(t *testing.T) {
    store := product.NewMockProductStore()
    svc := product.NewProductService(store, &product.MockShopifyClient{})

    err := svc.UpdateProductStatus(context.Background(), "any-id", "invalid_status")
    if err == nil { t.Fatal("expected validation error for invalid status") }
}
```

**Step 2: Verify test fails**
```
Run: go test ./internal/product/... -run "TestGetProductByID|TestUpdateProductStatus" -v
Expected: FAIL — methods undefined on ProductService interface
```

**Step 3: Write minimal implementation**
```go
// internal/product/service.go — additions

type ProductDetailDTO struct {
    ID          string `json:"id"`
    ShopifyID   string `json:"shopify_id"`
    Title       string `json:"title"`
    Description string `json:"description"`
    Status      string `json:"status"`
}

// Add to ProductService interface:
GetProductByID(ctx context.Context, id string) (*ProductDetailDTO, error)
GetVariantsByProductID(ctx context.Context, productID string) ([]VariantDTO, error)
UpdateProductStatus(ctx context.Context, productID string, status string) error
UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string) error
UpdateVariantPrice(ctx context.Context, variantID string, wsPrice *float64, retailPrice *float64) error

// Implementations on *service:

var validProductStatuses = map[string]bool{"active": true, "draft": true, "archived": true}

func (s *service) GetProductByID(ctx context.Context, id string) (*ProductDetailDTO, error) {
    p, err := s.store.GetProductByID(ctx, id)
    if err != nil { return nil, apierror.ErrInternal }
    if p == nil { return nil, apierror.ErrNotFound }
    return &ProductDetailDTO{ID: p.ID, ShopifyID: p.ShopifyID, Title: p.Title, Description: p.Description, Status: p.Status}, nil
}

func (s *service) GetVariantsByProductID(ctx context.Context, productID string) ([]VariantDTO, error) {
    variants, err := s.store.GetVariantsByProductID(ctx, productID)
    if err != nil { return nil, apierror.ErrInternal }
    dtos := make([]VariantDTO, len(variants))
    for i, v := range variants { dtos[i] = *mapVariantToDTO(&v) }
    return dtos, nil
}

func (s *service) UpdateProductStatus(ctx context.Context, productID string, status string) error {
    if !validProductStatuses[status] {
        return apierror.New(400, "validation_error", "status must be one of: active, draft, archived")
    }
    if err := s.store.UpdateProductStatus(ctx, productID, status); err != nil {
        return apierror.ErrInternal
    }
    return nil
}

func (s *service) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string) error {
    if err := s.store.UpdateVariantBatchLabel(ctx, productID, batchLabel); err != nil {
        return apierror.ErrInternal
    }
    return nil
}

func (s *service) UpdateVariantPrice(ctx context.Context, variantID string, wsPrice *float64, retailPrice *float64) error {
    if wsPrice == nil && retailPrice == nil {
        return apierror.New(400, "validation_error", "at least one of ws_price or retail_price must be provided")
    }
    if err := s.store.UpdateVariantPrices(ctx, variantID, wsPrice, retailPrice); err != nil {
        return apierror.ErrInternal
    }
    return nil
}
```

**Step 4: Verify test passes**
```
Run: go test ./internal/product/... -run "TestGetProductByID|TestUpdateProductStatus" -v
Expected: PASS with exit code 0
```

---

### Task 10: Wire Missing API Contract Endpoints in Handler

**Files:**
- Modify: `internal/product/handler.go`

**Requirements:**

- **Acceptance Criteria**
  1. `GET /api/v1/products/:id` → 200 with `ProductDetailDTO` or 404
  2. `GET /api/v1/products/:id/variants` → 200 with `[]VariantDTO` or 404
  3. `PATCH /api/v1/products/:id/status` → 200 or 400 (invalid status) — admin only
  4. `PATCH /api/v1/products/:id/batch` → 200 — admin only
  5. `PATCH /api/v1/products/variant/:id/price` → 200 or 400 (both nil) — admin only
  6. All routes present in `SetupRoutes`
  7. All handlers include Swagger `// @Summary` annotations

- **Functional Requirements**
  1. `GET` public routes are registered on the `group` (no auth middleware)
  2. `PATCH` admin routes are registered on the `admin` sub-group (after `Auth` + `RBAC("admin")`)
  3. Price PATCH request body: `{ "ws_price": float, "retail_price": float }` — both optional, at least one required
  4. Status PATCH request body: `{ "status": "active|draft|archived" }`
  5. Batch PATCH request body: `{ "batch_label": "string" }`
  6. All handlers log operation using `slog.InfoContext` at entry with `productID` field

- **Non-Functional Requirements**
  - Route for `/products/variant/:id/price` must be registered BEFORE `/products/:id` to avoid Fiber parameter collision

- **Test Coverage**
  - [Unit] `TestGetProductByIDHandler_NotFound` — 404 response
  - [Unit] `TestPatchStatusHandler_InvalidStatus` — 400 response

**Step 1: Write failing test**
```go
// internal/product/handler_test.go — add
func TestGetProductByIDHandler_NotFound(t *testing.T) {
    app := fiber.New()
    store := product.NewMockProductStore()
    svc := product.NewProductService(store, &product.MockShopifyClient{})
    h := product.NewProductHandler(svc, "test-secret")
    api := app.Group("/api/v1")
    h.SetupRoutes(api)

    req := httptest.NewRequest("GET", "/api/v1/products/nonexistent-id", nil)
    resp, _ := app.Test(req)
    if resp.StatusCode != 404 {
        t.Fatalf("expected 404, got %d", resp.StatusCode)
    }
}
```

**Step 2: Verify test fails**
```
Run: go test ./internal/product/... -run TestGetProductByIDHandler_NotFound -v
Expected: FAIL — 404 route not registered, returns 404 but for wrong reason OR route missing returns 405
```

**Step 3: Write minimal implementation**
```go
// internal/product/handler.go — full updated SetupRoutes + new handlers

func (h *Handler) SetupRoutes(router fiber.Router) {
    group := router.Group("/products")

    // Public endpoints
    group.Get("/", h.GetProducts)
    group.Get("/:id/variants", h.GetProductVariants)   // must be before /:id
    group.Get("/:id", h.GetProductByID)

    // Admin endpoints
    admin := group.Group("/")
    admin.Use(middleware.Auth(h.jwtSecret))
    admin.Use(middleware.RBAC("admin"))
    admin.Post("/sync", h.SyncProducts)
    admin.Patch("/variant/:id/price", h.UpdateVariantPrice)
    admin.Patch("/:id/status", h.UpdateProductStatus)
    admin.Patch("/:id/batch", h.UpdateVariantBatchLabel)
}

// GetProductByID godoc
// @Summary Get product by ID
// @Tags Product
// @Produce json
// @Param id path string true "Product ID"
// @Success 200 {object} response.Envelope{data=ProductDetailDTO}
// @Failure 404 {object} response.Envelope{error=response.ErrorBlock}
// @Router /products/{id} [get]
func (h *Handler) GetProductByID(c *fiber.Ctx) error {
    slog.InfoContext(c.Context(), "GetProductByID", slog.String("product_id", c.Params("id")))
    dto, err := h.service.GetProductByID(c.Context(), c.Params("id"))
    if err != nil { return response.Error(c, err) }
    return response.Success(c, fiber.StatusOK, "Product retrieved", dto)
}

// GetProductVariants godoc
// @Summary Get variants for a product
// @Tags Product
// @Produce json
// @Param id path string true "Product ID"
// @Success 200 {object} response.Envelope{data=[]VariantDTO}
// @Router /products/{id}/variants [get]
func (h *Handler) GetProductVariants(c *fiber.Ctx) error {
    slog.InfoContext(c.Context(), "GetProductVariants", slog.String("product_id", c.Params("id")))
    variants, err := h.service.GetVariantsByProductID(c.Context(), c.Params("id"))
    if err != nil { return response.Error(c, err) }
    return response.Success(c, fiber.StatusOK, "Variants retrieved", variants)
}

// UpdateProductStatus godoc
// @Summary Update product status
// @Tags Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Success 200 {object} response.Envelope
// @Failure 400 {object} response.Envelope{error=response.ErrorBlock}
// @Router /products/{id}/status [patch]
func (h *Handler) UpdateProductStatus(c *fiber.Ctx) error {
    slog.InfoContext(c.Context(), "UpdateProductStatus", slog.String("product_id", c.Params("id")))
    var req struct {
        Status string `json:"status" validate:"required"`
    }
    if err := c.BodyParser(&req); err != nil { return response.Error(c, err) }
    if err := validator.ValidateStruct(&req); err != nil { return response.Error(c, err) }
    if err := h.service.UpdateProductStatus(c.Context(), c.Params("id"), req.Status); err != nil {
        return response.Error(c, err)
    }
    return response.Success(c, fiber.StatusOK, "Product status updated", nil)
}

// UpdateVariantBatchLabel godoc
// @Summary Update preorder batch label for all variants of a product
// @Tags Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID"
// @Router /products/{id}/batch [patch]
func (h *Handler) UpdateVariantBatchLabel(c *fiber.Ctx) error {
    slog.InfoContext(c.Context(), "UpdateVariantBatchLabel", slog.String("product_id", c.Params("id")))
    var req struct {
        BatchLabel string `json:"batch_label" validate:"required"`
    }
    if err := c.BodyParser(&req); err != nil { return response.Error(c, err) }
    if err := validator.ValidateStruct(&req); err != nil { return response.Error(c, err) }
    if err := h.service.UpdateVariantBatchLabel(c.Context(), c.Params("id"), req.BatchLabel); err != nil {
        return response.Error(c, err)
    }
    return response.Success(c, fiber.StatusOK, "Batch label updated", nil)
}

// UpdateVariantPrice godoc
// @Summary Override ws_price and/or retail_price for a variant
// @Tags Product
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Variant ID"
// @Router /products/variant/{id}/price [patch]
func (h *Handler) UpdateVariantPrice(c *fiber.Ctx) error {
    slog.InfoContext(c.Context(), "UpdateVariantPrice", slog.String("variant_id", c.Params("id")))
    var req struct {
        WSPrice     *float64 `json:"ws_price"`
        RetailPrice *float64 `json:"retail_price"`
    }
    if err := c.BodyParser(&req); err != nil { return response.Error(c, err) }
    if err := h.service.UpdateVariantPrice(c.Context(), c.Params("id"), req.WSPrice, req.RetailPrice); err != nil {
        return response.Error(c, err)
    }
    return response.Success(c, fiber.StatusOK, "Variant price updated", nil)
}
```

**Step 4: Verify test passes**
```
Run: go test ./internal/product/... -run "TestGetProductByIDHandler|TestPatchStatusHandler" -v
Expected: PASS with exit code 0
```

---

## Final Verification

```bash
# 1. All unit tests pass (no infrastructure required)
go test ./internal/product/... -v

# 2. Integration tests pass (requires running Postgres)
go test -tags integration ./internal/product/... -v

# 3. Full build clean
go build ./...

# 4. No type assertions in service layer
grep -rn "(\*Postgres" internal/product/service.go
# Expected: no output

# 5. RBAC enforced on sync
grep -n "RBAC" internal/product/handler.go
# Expected: at least one line

# 6. Swagger regenerated
swag init -g cmd/server/main.go --parseDependency --parseInternal
```

### Manual E2E Test Sequence

```
1. POST /api/v1/auth/login             → copy access_token (admin role)
2. POST /api/v1/products/sync          → Authorization: Bearer <admin_token>  → expect 200
3. SELECT COUNT(*) FROM product_variants;                                       → expect > 0
4. GET  /api/v1/products               → expect list of variants
5. GET  /api/v1/products/<product_id>  → expect ProductDetailDTO
6. GET  /api/v1/products/<product_id>/variants → expect []VariantDTO
7. POST /api/v1/auth/login (customer)  → copy customer_token
8. POST /api/v1/products/sync          → Authorization: Bearer <customer_token> → expect 403
```
