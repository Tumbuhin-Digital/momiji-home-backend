ALTER TABLE preorder_shipments
  ADD COLUMN IF NOT EXISTS warehouse_origin VARCHAR(10) NOT NULL DEFAULT 'east';
