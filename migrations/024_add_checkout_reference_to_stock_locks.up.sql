ALTER TABLE stock_locks
  ADD COLUMN IF NOT EXISTS checkout_reference UUID;

CREATE INDEX IF NOT EXISTS idx_stock_locks_checkout_reference
  ON stock_locks(checkout_reference)
  WHERE checkout_reference IS NOT NULL;
