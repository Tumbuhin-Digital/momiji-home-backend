-- Collapse to a single shipment per order (keep unbatched / first row).
DELETE FROM preorder_shipments ps
WHERE ps.id NOT IN (
    SELECT DISTINCT ON (order_id) id
    FROM preorder_shipments
    ORDER BY order_id, (batch_id IS NOT NULL), created_at ASC
);

DROP INDEX IF EXISTS idx_preorder_packing_shipment_line;
DROP INDEX IF EXISTS idx_preorder_shipments_batch_id;
DROP INDEX IF EXISTS idx_preorder_shipments_order_unbatched;
DROP INDEX IF EXISTS idx_preorder_shipments_order_batch;

ALTER TABLE preorder_packing_items
    DROP COLUMN IF EXISTS quantity;

ALTER TABLE preorder_packing_items
    ADD CONSTRAINT preorder_packing_items_order_line_item_id_key UNIQUE (order_line_item_id);

ALTER TABLE preorder_shipments
    DROP COLUMN IF EXISTS batch_id;

ALTER TABLE preorder_shipments
    ADD CONSTRAINT preorder_shipments_order_id_key UNIQUE (order_id);
