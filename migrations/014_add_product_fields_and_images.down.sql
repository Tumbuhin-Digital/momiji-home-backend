-- +migrate Down
DROP TABLE product_images;

ALTER TABLE products DROP COLUMN handle;
ALTER TABLE products DROP COLUMN vendor;
ALTER TABLE products DROP COLUMN product_type;
ALTER TABLE products DROP COLUMN tags;
ALTER TABLE products DROP COLUMN body_html;
