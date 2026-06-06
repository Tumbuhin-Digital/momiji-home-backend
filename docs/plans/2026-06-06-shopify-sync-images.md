# Fix Missing Product Images

Currently, `ProductDTO` returns an empty `Images []` array because product-level images are never fetched from Shopify, never stored in the database, and never preloaded in the queries. This plan fixes the sync pipeline to retrieve and serve product images.

## Open Questions

None. The DB schema (`ProductImage`) is already prepared for this, we just need to populate it.

## Proposed Changes

---

### Shopify Sync

Summary of what will change in this component, separated by files.

#### [MODIFY] [service.go](file:///home/charmingnim/Work/Tumbuhin/code/momiji-home-backend/internal/product/service.go)
1. **GraphQL Query**: Add the `images` block to the `products` node.
   ```graphql
   images(first: 10) {
     edges {
       node { id url altText }
     }
   }
   ```
2. **Response Struct**: Add the `Images` struct matching the new GraphQL payload.
3. **Sync Loop**: After upserting the product, loop through `pNode.Images.Edges` and construct a slice of `ProductImage`. Pass this slice to a new `UpsertProductImages` method in the store.

---

### Postgres Store

#### [MODIFY] [postgres.go](file:///home/charmingnim/Work/Tumbuhin/code/momiji-home-backend/internal/product/postgres.go)
1. **Preloading**: Add `.Preload("Images")` to:
   - `GetProducts`
   - `GetProductByID`
2. **New Method `UpsertProductImages`**: Implement batch upsert for `ProductImage` resolving conflicts on `shopify_id`.
   ```go
   func (s *PostgresStore) UpsertProductImages(ctx context.Context, productID string, images []ProductImage) error {
       // Clear old images not in the new list (or simply upsert)
       // Insert/Update new images
   }
   ```

#### [MODIFY] [store.go](file:///home/charmingnim/Work/Tumbuhin/code/momiji-home-backend/internal/product/store.go)
1. Add `UpsertProductImages(ctx context.Context, productID string, images []ProductImage) error` to the `Store` interface.

---

### Service DTO Mapping

#### [MODIFY] [service.go](file:///home/charmingnim/Work/Tumbuhin/code/momiji-home-backend/internal/product/service.go)
1. In `mapProductToDTO`, loop through `p.Images` and map them to `ImageDTO`.

## Verification Plan

### Manual Verification
1. Run `POST /api/v1/products/sync` to trigger the Shopify sync.
2. Verify in the database that `product_images` is populated.
3. Call `GET /api/v1/products` and verify the `images` array is populated with URLs.
