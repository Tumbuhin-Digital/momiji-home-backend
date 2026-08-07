DROP INDEX IF EXISTS idx_products_create_idempotency_key;

ALTER TABLE product_variants
    DROP COLUMN IF EXISTS inventory_tracked,
    DROP COLUMN IF EXISTS custom_link_state;

ALTER TABLE products
    DROP COLUMN IF EXISTS create_idempotency_key,
    DROP COLUMN IF EXISTS internal_note,
    DROP COLUMN IF EXISTS origin;
