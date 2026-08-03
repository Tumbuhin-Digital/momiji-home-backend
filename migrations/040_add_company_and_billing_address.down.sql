ALTER TABLE orders DROP CONSTRAINT IF EXISTS fk_billing_address;
ALTER TABLE orders DROP COLUMN IF EXISTS billing_address_id;
ALTER TABLE customer_addresses DROP COLUMN IF EXISTS company;
