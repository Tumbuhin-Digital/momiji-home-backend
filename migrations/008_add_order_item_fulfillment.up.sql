ALTER TABLE order_items ADD COLUMN fulfillment_step INT NOT NULL DEFAULT 1;
ALTER TABLE order_items ADD COLUMN items_received INT NOT NULL DEFAULT 0;
