ALTER TABLE order_line_items DROP COLUMN IF EXISTS title;
ALTER TABLE order_line_items DROP COLUMN IF EXISTS unit_price;
ALTER TABLE order_line_items DROP COLUMN IF EXISTS amount_charged;
ALTER TABLE order_line_items DROP COLUMN IF EXISTS balance_due;
ALTER TABLE order_line_items DROP COLUMN IF EXISTS fulfillment_step;
ALTER TABLE order_line_items DROP COLUMN IF EXISTS items_received;
ALTER TABLE order_line_items RENAME TO order_items;
