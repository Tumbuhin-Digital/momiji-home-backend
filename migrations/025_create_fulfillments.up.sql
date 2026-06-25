CREATE TABLE fulfillment_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    shopify_fulfillment_order_id VARCHAR(128) NOT NULL UNIQUE,
    status VARCHAR(32) NOT NULL DEFAULT 'OPEN',
    assigned_location_name VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_fulfillment_orders_order_id ON fulfillment_orders(order_id);

CREATE TABLE fulfillment_order_line_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fulfillment_order_id UUID NOT NULL REFERENCES fulfillment_orders(id) ON DELETE CASCADE,
    order_line_item_id UUID NOT NULL REFERENCES order_line_items(id) ON DELETE CASCADE,
    shopify_fulfillment_order_line_item_id VARCHAR(128) NOT NULL UNIQUE,
    total_quantity INT NOT NULL,
    remaining_quantity INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_foli_fulfillment_order_id ON fulfillment_order_line_items(fulfillment_order_id);
CREATE INDEX idx_foli_order_line_item_id ON fulfillment_order_line_items(order_line_item_id);

CREATE TABLE fulfillments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    shopify_fulfillment_id VARCHAR(128) UNIQUE,
    sequence_number INT NOT NULL DEFAULT 1,
    tracking_number VARCHAR(128),
    tracking_url VARCHAR(512),
    tracking_company VARCHAR(128),
    shipment_status VARCHAR(64),
    status VARCHAR(32) NOT NULL DEFAULT 'fulfilled',
    notify_customer BOOLEAN NOT NULL DEFAULT false,
    fulfilled_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_fulfillments_order_id ON fulfillments(order_id);

CREATE TABLE fulfillment_line_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fulfillment_id UUID NOT NULL REFERENCES fulfillments(id) ON DELETE CASCADE,
    order_line_item_id UUID NOT NULL REFERENCES order_line_items(id) ON DELETE CASCADE,
    quantity INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_fli_fulfillment_id ON fulfillment_line_items(fulfillment_id);

CREATE TABLE shopify_webhook_events (
    webhook_id VARCHAR(128) PRIMARY KEY,
    topic VARCHAR(128) NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
