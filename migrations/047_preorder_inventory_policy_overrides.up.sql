-- Variants whose "Sell when out of stock" (inventoryPolicy CONTINUE) the app turned on so a
-- pre-order balance can be paid while Shopify stock is short. One row per group and variant.
-- released_at: the group no longer needs it (balance paid or pre-order cancelled).
-- restored_at: the app set the variant back to DENY (only once no group needs it).
-- A variant that was already CONTINUE before the app touched it never gets a row, so the
-- app never turns it off.
CREATE TABLE IF NOT EXISTS preorder_inventory_policy_overrides (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    shipment_id        UUID NOT NULL REFERENCES preorder_shipments(id) ON DELETE CASCADE,
    shopify_variant_id VARCHAR(128) NOT NULL,
    shopify_product_id VARCHAR(128) NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at        TIMESTAMPTZ,
    restored_at        TIMESTAMPTZ,
    UNIQUE (shipment_id, shopify_variant_id)
);

CREATE INDEX IF NOT EXISTS idx_preorder_policy_overrides_variant_open
    ON preorder_inventory_policy_overrides(shopify_variant_id)
    WHERE restored_at IS NULL;
