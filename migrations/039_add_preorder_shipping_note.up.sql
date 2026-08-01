INSERT INTO app_settings (key, value) VALUES
  ('checkout_preorder_shipping_note',
   'You will be notified when our next shipment arrives in the US')
ON CONFLICT (key) DO NOTHING;
