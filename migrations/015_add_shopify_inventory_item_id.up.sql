ALTER TABLE product_variants ADD COLUMN shopify_inventory_item_id VARCHAR;
ALTER TABLE order_line_items ADD COLUMN draft_order_id VARCHAR;
