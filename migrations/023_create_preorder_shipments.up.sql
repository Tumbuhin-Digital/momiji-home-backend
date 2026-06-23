CREATE TABLE IF NOT EXISTS preorder_shipments (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id            UUID NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
    estimated_shipping  NUMERIC(10, 2),
    final_shipping_price NUMERIC(10, 2),
    shipping_notes      TEXT,
    credit_amount       NUMERIC(10, 2) NOT NULL DEFAULT 0,
    total_boxes         INT NOT NULL DEFAULT 0,
    total_weight_lb     NUMERIC(10, 2),
    invoice_sent_at     TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS preorder_packing_items (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    preorder_shipment_id    UUID NOT NULL REFERENCES preorder_shipments(id) ON DELETE CASCADE,
    order_line_item_id      UUID NOT NULL UNIQUE REFERENCES order_line_items(id) ON DELETE CASCADE,
    box_count               INT NOT NULL DEFAULT 0,
    is_nested               BOOLEAN NOT NULL DEFAULT FALSE,
    nested_in_line_item_id  UUID REFERENCES order_line_items(id) ON DELETE SET NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_preorder_packing_items_shipment_id
    ON preorder_packing_items(preorder_shipment_id);
