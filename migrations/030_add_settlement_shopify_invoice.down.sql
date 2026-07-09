ALTER TABLE preorder_settlements
  DROP COLUMN IF EXISTS shopify_invoice_url,
  DROP COLUMN IF EXISTS shopify_draft_order_id;
