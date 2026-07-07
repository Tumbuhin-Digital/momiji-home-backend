BEGIN;

DELETE FROM app_settings
WHERE key IN ('store_closed', 'store_closed_message');

COMMIT;

