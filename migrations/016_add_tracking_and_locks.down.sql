ALTER TABLE order_line_items
  DROP COLUMN IF EXISTS tracking_number,
  DROP COLUMN IF EXISTS tracking_url,
  DROP COLUMN IF EXISTS shipped_at;

DROP TABLE IF EXISTS stock_locks;
