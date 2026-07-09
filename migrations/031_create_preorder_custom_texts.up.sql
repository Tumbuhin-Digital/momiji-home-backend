BEGIN;

CREATE TABLE IF NOT EXISTS preorder_custom_texts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    label       VARCHAR(128) NOT NULL,
    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS preorder_custom_texts_label_active_idx
    ON preorder_custom_texts (LOWER(label))
    WHERE deleted_at IS NULL;

INSERT INTO preorder_custom_texts (label)
SELECT DISTINCT TRIM(preorder_batch_label)
FROM product_variants
WHERE preorder_batch_label IS NOT NULL
  AND TRIM(preorder_batch_label) <> ''
  AND NOT EXISTS (
    SELECT 1 FROM preorder_custom_texts pct
    WHERE LOWER(pct.label) = LOWER(TRIM(product_variants.preorder_batch_label))
      AND pct.deleted_at IS NULL
  );

COMMIT;
