DROP INDEX IF EXISTS idx_fulfillments_batch_id;

ALTER TABLE fulfillments
    DROP COLUMN IF EXISTS batch_id;
