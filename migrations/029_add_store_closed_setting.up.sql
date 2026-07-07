BEGIN;

INSERT INTO app_settings (key, value) VALUES
  ('store_closed', 'false'),
  ('store_closed_message', 'Store is currently closed. Checkout is temporarily unavailable.')
ON CONFLICT (key) DO NOTHING;

COMMIT;

