BEGIN;

CREATE TABLE IF NOT EXISTS app_settings (
    key         VARCHAR(100) PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO app_settings (key, value) VALUES
  ('checkout_due_now_note',   '1-2 business days dispatch, UPS Ground or equivalent carrier'),
  ('checkout_due_later_note', 'You will be notified when our next shipment arrives in the US')
ON CONFLICT (key) DO NOTHING;

COMMIT;
