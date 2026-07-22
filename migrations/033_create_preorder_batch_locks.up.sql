CREATE TABLE IF NOT EXISTS preorder_batch_locks (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  batch_id            UUID NOT NULL REFERENCES preorder_batches(id) ON DELETE CASCADE,
  shopify_variant_id  VARCHAR(64) NOT NULL,
  quantity            INT NOT NULL CHECK (quantity > 0),
  session_id          VARCHAR(64),
  user_id             UUID,
  checkout_reference  VARCHAR(64),
  expires_at          TIMESTAMPTZ NOT NULL,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_batch_locks_batch_expires
  ON preorder_batch_locks(batch_id, expires_at);

CREATE INDEX IF NOT EXISTS idx_batch_locks_checkout_ref
  ON preorder_batch_locks(checkout_reference);

CREATE INDEX IF NOT EXISTS idx_batch_locks_variant_expires
  ON preorder_batch_locks(shopify_variant_id, expires_at);
