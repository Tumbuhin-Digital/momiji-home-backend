CREATE TABLE products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shopify_id VARCHAR(64) UNIQUE NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE product_variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    shopify_variant_id VARCHAR(64) UNIQUE NOT NULL,
    title VARCHAR(255) NOT NULL,
    sku VARCHAR(128),
    price NUMERIC(12,2) NOT NULL,
    image_src TEXT,
    retail_price NUMERIC(12,2),
    ws_price NUMERIC(12,2),
    fulfillment_type VARCHAR(32) NOT NULL DEFAULT 'ship_ready',
    preorder_batch_label VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
