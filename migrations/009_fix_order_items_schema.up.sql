ALTER TABLE order_items RENAME TO order_line_items;
ALTER TABLE order_line_items ADD COLUMN title VARCHAR(255);
ALTER TABLE order_line_items ADD COLUMN unit_price NUMERIC(12,2);
ALTER TABLE order_line_items ADD COLUMN amount_charged NUMERIC(12,2);
ALTER TABLE order_line_items ADD COLUMN balance_due NUMERIC(12,2);
ALTER TABLE order_line_items ADD COLUMN fulfillment_step INT NOT NULL DEFAULT 1;
ALTER TABLE order_line_items ADD COLUMN items_received INT NOT NULL DEFAULT 0;
