CREATE TABLE order_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id),
    shopify_variant_id VARCHAR(64) NOT NULL,
    type VARCHAR(16) NOT NULL,
    quantity INT NOT NULL,
    item_status VARCHAR(32) NOT NULL,
    dp_amount NUMERIC(12,2),
    final_amount NUMERIC(12,2),
    shopify_order_id VARCHAR(64),
    tracking_number VARCHAR(128),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
