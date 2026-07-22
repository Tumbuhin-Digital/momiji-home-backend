DROP INDEX IF EXISTS idx_preorder_shipments_draft_order_id;

ALTER TABLE preorder_shipments
    DROP COLUMN IF EXISTS invoice_paid_at;

ALTER TABLE preorder_shipments
    DROP COLUMN IF EXISTS invoice_url;

ALTER TABLE preorder_shipments
    DROP COLUMN IF EXISTS shopify_draft_order_id;
