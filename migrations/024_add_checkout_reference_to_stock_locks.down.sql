DROP INDEX IF EXISTS idx_stock_locks_checkout_reference;

ALTER TABLE stock_locks
  DROP COLUMN IF EXISTS checkout_reference;
