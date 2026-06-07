ALTER TABLE product_variants
  DROP COLUMN IF EXISTS weight_kg,
  DROP COLUMN IF EXISTS width_cm,
  DROP COLUMN IF EXISTS height_cm,
  DROP COLUMN IF EXISTS depth_cm;

ALTER TABLE product_variants
  ALTER COLUMN fulfillment_type DROP DEFAULT;
