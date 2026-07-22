-- Per-group second payment invoice tracking on preorder shipments.
ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS shopify_draft_order_id VARCHAR(128);

ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS invoice_url TEXT;

ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS invoice_paid_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_preorder_shipments_draft_order_id
    ON preorder_shipments (shopify_draft_order_id)
    WHERE shopify_draft_order_id IS NOT NULL;
