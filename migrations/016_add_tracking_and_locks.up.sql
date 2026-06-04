ALTER TABLE order_line_items
  ADD COLUMN IF NOT EXISTS tracking_number VARCHAR(255),
  ADD COLUMN IF NOT EXISTS tracking_url    VARCHAR(512),
  ADD COLUMN IF NOT EXISTS shipped_at      TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS stock_locks (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  shopify_variant_id VARCHAR(255) NOT NULL,
  quantity        INT NOT NULL,
  session_id      VARCHAR(255),
  user_id         UUID,
  expires_at      TIMESTAMPTZ NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_stock_locks_variant ON stock_locks(shopify_variant_id, expires_at);
