-- Pre-order balance billed by editing the deposit order (one Shopify order per purchase)
-- instead of a second draft-order invoice. See docs/prd/pre-order-revisi-satu-order.md.

-- The Shopify line item each order line came from. For pre-order lines this is the deposit
-- line that the balance edit reduces.
ALTER TABLE order_line_items
    ADD COLUMN IF NOT EXISTS shopify_line_item_id VARCHAR(128);

-- How a group's balance was billed, and what the order edit left behind.
ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS balance_method VARCHAR(20),
    ADD COLUMN IF NOT EXISTS balance_expected_outstanding NUMERIC(12, 2),
    ADD COLUMN IF NOT EXISTS shopify_shipping_line_id VARCHAR(128);

COMMENT ON COLUMN preorder_shipments.balance_method IS
    'NULL or draft_order: balance billed as a separate draft-order invoice (legacy). order_edit: balance added to the deposit order by an order edit.';
COMMENT ON COLUMN preorder_shipments.balance_expected_outstanding IS
    'Outstanding amount the deposit order must show once this group''s balance edit is committed; used to adopt an edit whose save failed.';
COMMENT ON COLUMN preorder_shipments.shopify_shipping_line_id IS
    'Shipping line the balance edit added for this group''s remaining shipping, so a re-price can replace it.';

-- Real product lines added to the deposit order by a balance edit. Lines added by an edit
-- cannot carry custom attributes, so this is the only link back to the app's line items.
CREATE TABLE IF NOT EXISTS preorder_balance_lines (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shipment_id          UUID NOT NULL REFERENCES preorder_shipments(id) ON DELETE CASCADE,
    order_line_item_id   UUID NOT NULL REFERENCES order_line_items(id) ON DELETE CASCADE,
    shopify_line_item_id VARCHAR(128) NOT NULL UNIQUE,
    quantity             INT NOT NULL,
    unit_price           NUMERIC(12, 2) NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_preorder_balance_lines_shipment ON preorder_balance_lines(shipment_id);
CREATE INDEX IF NOT EXISTS idx_preorder_balance_lines_line_item ON preorder_balance_lines(order_line_item_id);

-- Which Shopify line a fulfillment order line belongs to, and whether it is the product
-- itself or a money-only deposit line, so tracking lands on the product line.
ALTER TABLE fulfillment_order_line_items
    ADD COLUMN IF NOT EXISTS shopify_line_item_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS kind VARCHAR(20);
