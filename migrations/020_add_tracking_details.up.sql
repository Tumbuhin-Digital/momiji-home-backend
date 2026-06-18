BEGIN;

ALTER TABLE order_line_items
  ADD COLUMN IF NOT EXISTS tracking_company VARCHAR(128),
  ADD COLUMN IF NOT EXISTS tracking_last_event VARCHAR(255);

COMMIT;
