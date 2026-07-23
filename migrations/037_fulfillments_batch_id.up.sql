-- Scope fulfillments to a pre-order batch (NULL = unbatched / legacy).
ALTER TABLE fulfillments
    ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES preorder_batches(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_fulfillments_batch_id ON fulfillments(batch_id);
