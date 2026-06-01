ALTER TABLE product_variants DROP COLUMN IF EXISTS shopify_inventory_item_id;
ALTER TABLE order_line_items DROP COLUMN IF EXISTS draft_order_id;
