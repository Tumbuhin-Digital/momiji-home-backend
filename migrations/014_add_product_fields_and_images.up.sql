-- +migrate Up
ALTER TABLE products ADD COLUMN handle VARCHAR(255);
ALTER TABLE products ADD COLUMN vendor VARCHAR(255);
ALTER TABLE products ADD COLUMN product_type VARCHAR(255);
ALTER TABLE products ADD COLUMN tags TEXT;
ALTER TABLE products ADD COLUMN body_html TEXT;

CREATE TABLE product_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    shopify_id VARCHAR(255) UNIQUE NOT NULL,
    src TEXT NOT NULL,
    alt TEXT,
    position INT NOT NULL DEFAULT 1
);
