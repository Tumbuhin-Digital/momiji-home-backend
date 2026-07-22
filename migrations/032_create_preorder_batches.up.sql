BEGIN;

CREATE TABLE IF NOT EXISTS preorder_batches (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    variant_id         UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    shopify_product_id VARCHAR(64) NOT NULL,
    name               VARCHAR(128) NOT NULL,
    qty_allocated      INT NOT NULL CHECK (qty_allocated >= 1),
    qty_sold           INT NOT NULL DEFAULT 0 CHECK (qty_sold >= 0),
    sequence           INT NOT NULL,
    status             VARCHAR(16) NOT NULL CHECK (status IN ('active', 'queued', 'closed', 'cancelled')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at          TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_preorder_batches_variant_name
    ON preorder_batches (variant_id, LOWER(name));

CREATE UNIQUE INDEX IF NOT EXISTS uq_preorder_batches_one_active_per_variant
    ON preorder_batches (variant_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_preorder_batches_variant_status_sequence
    ON preorder_batches (variant_id, status, sequence);

CREATE TABLE IF NOT EXISTS preorder_batch_allocations (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id           UUID NOT NULL REFERENCES preorder_batches(id) ON DELETE CASCADE,
    order_line_item_id UUID REFERENCES order_line_items(id) ON DELETE SET NULL,
    shopify_variant_id VARCHAR(64) NOT NULL,
    quantity           INT NOT NULL CHECK (quantity > 0),
    status             VARCHAR(16) NOT NULL DEFAULT 'committed' CHECK (status IN ('committed', 'released')),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    released_at        TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_preorder_batch_allocations_order_line_item
    ON preorder_batch_allocations (order_line_item_id);

CREATE INDEX IF NOT EXISTS idx_preorder_batch_allocations_batch_status
    ON preorder_batch_allocations (batch_id, status);

COMMIT;
