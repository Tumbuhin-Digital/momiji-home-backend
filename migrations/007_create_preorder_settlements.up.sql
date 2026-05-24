CREATE TABLE preorder_settlements (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',  -- pending | invoiced | paid
    amount      NUMERIC(12,2) NOT NULL,
    invoiced_at TIMESTAMPTZ,
    paid_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_settlements_order_id ON preorder_settlements(order_id);
CREATE INDEX idx_settlements_status   ON preorder_settlements(status);
