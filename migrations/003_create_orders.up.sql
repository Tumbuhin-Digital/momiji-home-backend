CREATE TABLE orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shopify_order_id VARCHAR(64),
    shopify_draft_order_id VARCHAR(64),
    customer_id UUID NOT NULL REFERENCES users(id),
    total_price NUMERIC(12,2) NOT NULL,
    aggregate_status VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
