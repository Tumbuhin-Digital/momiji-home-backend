-- A balance edit that consumes the whole checkout shipping prepayment removes the upfront
-- "Shipping (Pre-Order) - 50%" line and bills the group's shipping as one full shipping
-- line. See docs/prd/pre-order-revisi-satu-order.md.
ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS balance_shipping_deposit_folded NUMERIC(12, 2) NOT NULL DEFAULT 0;

COMMENT ON COLUMN preorder_shipments.balance_shipping_deposit_folded IS
    'Upfront shipping line amount the balance edit removed and added back into this group''s shipping line; a re-price keeps it in the new line.';
