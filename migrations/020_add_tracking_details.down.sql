BEGIN;

ALTER TABLE order_items
  DROP COLUMN IF EXISTS tracking_company,
  DROP COLUMN IF EXISTS tracking_last_event;

COMMIT;
