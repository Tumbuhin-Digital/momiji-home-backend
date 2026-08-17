ALTER TABLE preorder_shipments
    ADD COLUMN IF NOT EXISTS prepaid_shipping NUMERIC(12, 2) NOT NULL DEFAULT 0;

COMMENT ON COLUMN preorder_shipments.prepaid_shipping IS
    'Shipping already collected at checkout (50% of the carrier estimate). Settlement bills final_shipping_price minus this. 0 for orders placed before the split-shipping scheme.';
