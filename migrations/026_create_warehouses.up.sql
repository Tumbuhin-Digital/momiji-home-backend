BEGIN;

CREATE TABLE IF NOT EXISTS warehouses (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code                      VARCHAR(10) NOT NULL UNIQUE CHECK (code IN ('east', 'west')),
    name                      VARCHAR(255) NOT NULL DEFAULT '',
    phone                     VARCHAR(50) NOT NULL DEFAULT '',
    address1                  VARCHAR(255) NOT NULL DEFAULT '',
    city                      VARCHAR(100) NOT NULL DEFAULT '',
    state                     VARCHAR(50) NOT NULL DEFAULT '',
    zip                       VARCHAR(20) NOT NULL DEFAULT '',
    country                   VARCHAR(10) NOT NULL DEFAULT 'US',
    shipstation_warehouse_id  VARCHAR(100),
    ground_service_code       VARCHAR(50),
    is_default                BOOLEAN NOT NULL DEFAULT false,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS warehouses_one_default ON warehouses (is_default) WHERE is_default = true;

INSERT INTO warehouses (
    code, name, phone, address1, city, state, zip, country, is_default
) VALUES (
    'east',
    'East Coast Warehouse',
    '',
    '',
    'Passaic',
    'NJ',
    '07055',
    'US',
    true
) ON CONFLICT (code) DO NOTHING;

INSERT INTO warehouses (
    code, name, phone, address1, city, state, zip, country, is_default
) VALUES (
    'west',
    'West Coast 3PL',
    '',
    '',
    '',
    '',
    '',
    'US',
    false
) ON CONFLICT (code) DO NOTHING;

COMMIT;
