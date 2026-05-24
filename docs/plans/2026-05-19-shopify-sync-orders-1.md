# Phase 4 Plan: Shopify Sync & Orders

> **Status:** ✅ COMPLETE  
> **Committed:** `4d12986 feat(order): implement orders service`, `1707910 feat(product): implement product service`, `ae58053 feat(shopify): add configuration for shopify integration`, `2fa8161 feat(product-table): add products and product_variants tables`  
> **Source:** `implementation_plan.md` at project root (preserved as-is)

---

## Goal

Now that the Shopify API tokens are provided, we will build out the core orchestration layer. This involves syncing product catalogs directly from Shopify into a local database, replacing the mock product data in the Cart module, and implementing the complex Order creation flow (splitting ready-to-ship checkouts and pre-order draft orders).

---

## Open Questions (at time of planning)

1. **Guest Order Strategy:** The contract states guest checkout is supported via `X-Session-ID` and `guest_info` (email, name, etc.). Upon guest checkout, should we automatically create a permanent `User` record for them in the database using their provided email, or should we modify the `orders` table to allow a nullable `customer_id` for pure anonymous orders? *(Recommendation: Create a User record implicitly to track their order history).*
2. **Webhooks:** The contract mentions webhooks for updating payment status. Should we build the webhook receivers (`/webhooks/shopify/*`) as part of this phase, or defer them to Phase 5?

---

## Proposed Changes

### 1. Database Migrations
#### [NEW] `migrations/006_create_products.up.sql`
- Create `products` table (shopify_id, title, etc).
- Create `product_variants` table (shopify_variant_id, product_id, sku, price, image_src).
- Add Momiji-specific fields to variants: `retail_price`, `ws_price`, `fulfillment_type`, `preorder_batch_label`.

### 2. Shopify Platform Client
#### [NEW] `internal/platform/shopify/client.go`
- Create an HTTP client abstraction that securely holds the `SHOPIFY_ADMIN_API_TOKEN` and `SHOPIFY_STOREFRONT_TOKEN`.
- Implement methods: `QueryAdminGraphQL`, `CreateDraftOrder`, `CreateStorefrontCheckout`.

### 3. Product Module (`internal/product/`)
#### [MODIFY] `internal/product/service.go`
- Replace `mockProductService` with a real `PostgresProductStore`.
- Implement a `SyncFromShopify` method that runs a GraphQL query against the Shopify Admin API to pull all products and upsert them into the local `product_variants` database.
#### [NEW] `internal/product/handler.go`
- Implement endpoints: `GET /products`, `POST /products/sync` (admin only), and `PATCH /products/variant/:variantId/price` to manage the local `ws_price` overrides.

### 4. Order Module (`internal/order/`)
#### [NEW] `internal/order/store.go` & `internal/order/postgres.go`
- Implement persistence for `Order` and `OrderItem` models.
#### [NEW] `internal/order/service.go`
- **Order Creation Logic**:
  1. Retrieve cart using session/user ID.
  2. If guest, create or fetch `User` using `guest_info.email`.
  3. If cart contains `ship_ready` items → trigger Shopify Storefront API to generate a checkout URL/ID.
  4. If cart contains `pre_order` items → trigger Shopify Admin API to generate a Draft Order (for 50% deposit).
  5. Save aggregated order to DB. Clear the Cart.
#### [NEW] `internal/order/handler.go`
- Implement endpoints: `POST /`, `GET /`, `GET /:id`, `PATCH /:id/accept`, `PATCH /:id/cancel`.

### 5. Routing Wiring
#### [MODIFY] `cmd/server/main.go`
- Initialize Shopify client using `.env` configurations.
- Initialize Product and Order handlers, wiring dependencies (CartService, ProductService, ShopifyClient).
- Mount `/api/v1/products` and `/api/v1/orders`.

---

## Verification Plan
1. **Product Sync:** Run `POST /api/v1/products/sync` and verify the local database is populated with Shopify variants.
2. **Cart Upgrade:** Add an item to the cart and verify it uses the real `ws_price` from the synced database rather than the old mock data.
3. **Order Creation:** Send a checkout payload, confirm it splits the cart successfully, generates a Shopify Checkout ID / Draft Order ID, and clears the active cart.
