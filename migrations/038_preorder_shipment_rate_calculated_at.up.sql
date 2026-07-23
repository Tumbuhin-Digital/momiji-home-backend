ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS rate_calculated_at TIMESTAMPTZ;
