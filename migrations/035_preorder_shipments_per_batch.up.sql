-- Allow multiple preorder shipments per order (one per batch / unbatched group).
ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES preorder_batches(id) ON DELETE SET NULL;

ALTER TABLE preorder_shipments
    DROP CONSTRAINT IF EXISTS preorder_shipments_order_id_key;

-- One shipment per (order, batch); NULL batch_id = unbatched pre-order group.
CREATE UNIQUE INDEX IF NOT EXISTS idx_preorder_shipments_order_batch
    ON preorder_shipments (order_id, batch_id)
    WHERE batch_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_preorder_shipments_order_unbatched
    ON preorder_shipments (order_id)
    WHERE batch_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_preorder_shipments_batch_id
    ON preorder_shipments (batch_id);

-- Packing rows may repeat the same line item across shipments (split qty).
ALTER TABLE preorder_packing_items
    ADD COLUMN IF NOT EXISTS quantity INT NOT NULL DEFAULT 0;

ALTER TABLE preorder_packing_items
    DROP CONSTRAINT IF EXISTS preorder_packing_items_order_line_item_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_preorder_packing_shipment_line
    ON preorder_packing_items (preorder_shipment_id, order_line_item_id);

-- Backfill packing quantity from full line qty when still zero.
UPDATE preorder_packing_items ppi
SET quantity = oli.quantity
FROM order_line_items oli
WHERE ppi.order_line_item_id = oli.id
  AND ppi.quantity = 0;

-- Attach legacy single shipment to the sole allocation batch when unambiguous.
UPDATE preorder_shipments ps
SET batch_id = sole.batch_id
FROM (
    SELECT oli.order_id, MIN(a.batch_id::text)::uuid AS batch_id
    FROM order_line_items oli
    JOIN preorder_batch_allocations a
      ON a.order_line_item_id = oli.id
     AND a.status = 'committed'
    WHERE oli.type = 'pre_order'
    GROUP BY oli.order_id
    HAVING COUNT(DISTINCT a.batch_id) = 1
) sole
WHERE ps.order_id = sole.order_id
  AND ps.batch_id IS NULL;
