ALTER TABLE products
    ADD COLUMN IF NOT EXISTS origin VARCHAR(32) NOT NULL DEFAULT 'shopify_sync',
    ADD COLUMN IF NOT EXISTS internal_note TEXT,
    ADD COLUMN IF NOT EXISTS create_idempotency_key VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS idx_products_create_idempotency_key
    ON products (create_idempotency_key)
    WHERE create_idempotency_key IS NOT NULL;

ALTER TABLE product_variants
    ADD COLUMN IF NOT EXISTS custom_link_state VARCHAR(32),
    ADD COLUMN IF NOT EXISTS inventory_tracked BOOLEAN NOT NULL DEFAULT TRUE;
