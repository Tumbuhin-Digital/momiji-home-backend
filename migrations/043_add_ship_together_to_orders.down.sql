ALTER TABLE orders
    DROP COLUMN IF EXISTS hold_until_batch,
    DROP COLUMN IF EXISTS ship_together;
