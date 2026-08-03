ALTER TABLE customer_addresses
    ADD COLUMN IF NOT EXISTS company VARCHAR(255);

ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS billing_address_id UUID;

ALTER TABLE orders
    ADD CONSTRAINT fk_billing_address
    FOREIGN KEY (billing_address_id) REFERENCES customer_addresses(id) ON DELETE SET NULL;
