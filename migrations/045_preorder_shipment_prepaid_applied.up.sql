ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS prepaid_applied NUMERIC(12, 2) NOT NULL DEFAULT 0;

COMMENT ON COLUMN preorder_shipments.prepaid_applied IS
    'How much of the order-level checkout prepayment this group''s settlement invoice consumed. The pool lives in prepaid_shipping on the checkout-created row and is shared across batch groups, so each group records what it actually used.';

-- Groups invoiced before this column existed deducted their own prepaid_shipping in full.
-- Record that as consumed so the pool is not handed out a second time.
UPDATE preorder_shipments
SET prepaid_applied = prepaid_shipping
WHERE invoice_sent_at IS NOT NULL
  AND prepaid_shipping > 0;
