ALTER TABLE preorder_settlements
  ADD COLUMN IF NOT EXISTS shopify_invoice_url TEXT,
  ADD COLUMN IF NOT EXISTS shopify_draft_order_id VARCHAR(128);
