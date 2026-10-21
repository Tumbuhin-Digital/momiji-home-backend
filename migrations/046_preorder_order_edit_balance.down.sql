ALTER TABLE fulfillment_order_line_items
    DROP COLUMN IF EXISTS kind,
    DROP COLUMN IF EXISTS shopify_line_item_id;

DROP TABLE IF EXISTS preorder_balance_lines;

ALTER TABLE preorder_shipments
    DROP COLUMN IF EXISTS shopify_shipping_line_id,
    DROP COLUMN IF EXISTS balance_expected_outstanding,
    DROP COLUMN IF EXISTS balance_method;

ALTER TABLE order_line_items
    DROP COLUMN IF EXISTS shopify_line_item_id;
