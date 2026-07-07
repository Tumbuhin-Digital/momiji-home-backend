package product

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) GetProducts(ctx context.Context, q ProductQuery) ([]Product, int64, error) {
	var products []Product
	var total int64

	query := s.db.WithContext(ctx).Model(&Product{})
	query = query.Where("LOWER(products.status) <> ?", "deleted")

	if q.Search != "" {
		searchPattern := "%" + q.Search + "%"
		query = query.Where("title ILIKE ? OR description ILIKE ?", searchPattern, searchPattern)
	}

	if q.FulfillmentType != "" {
		query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").
			Where("product_variants.fulfillment_type = ?", q.FulfillmentType).
			Group("products.id")
	}

	if q.ExcludeInactive {
		query = query.Where("products.id NOT IN (SELECT product_id FROM product_variants WHERE fulfillment_type = ?)", "inactive")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := q.Page
	if page < 1 {
		page = 1
	}
	limit := q.Limit
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	} // Fix 2D: Cap limit to 100
	offset := (page - 1) * limit

	orderStr := "products.created_at DESC" // Fix 1C: Default sorting
	switch q.Sort {
	case "price_asc":
		// Ensure join is present if not already added by fulfillment_type filter
		if q.FulfillmentType == "" {
			query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").Group("products.id")
		}
		orderStr = "MIN(COALESCE(product_variants.ws_price, product_variants.price)) ASC"
	case "price_desc":
		if q.FulfillmentType == "" {
			query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").Group("products.id")
		}
		orderStr = "MAX(COALESCE(product_variants.ws_price, product_variants.price)) DESC"
	case "name_asc":
		orderStr = "products.title ASC"
	case "created_at":
		orderStr = "products.created_at DESC"
	case "stock_asc":
		if q.FulfillmentType == "" {
			query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").Group("products.id")
		}
		orderStr = "SUM(product_variants.inventory_quantity) ASC"
	case "stock_desc":
		if q.FulfillmentType == "" {
			query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").Group("products.id")
		}
		orderStr = "SUM(product_variants.inventory_quantity) DESC"
	}

	err := query.Preload("Images").Preload("Variants").Order(orderStr).Offset(offset).Limit(limit).Find(&products).Error
	return products, total, err
}

func (s *PostgresStore) ListShopifyProductIDs(ctx context.Context) ([]string, error) {
	var ids []string
	if err := s.db.WithContext(ctx).Model(&Product{}).
		Where("LOWER(status) <> ?", "deleted").
		Pluck("shopify_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *PostgresStore) GetVariantByShopifyID(ctx context.Context, shopifyVariantID string) (*ProductVariant, error) {
	var variant ProductVariant
	err := s.db.WithContext(ctx).Where("shopify_variant_id = ?", shopifyVariantID).First(&variant).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &variant, nil
}

func (s *PostgresStore) GetVariantByInventoryItemID(ctx context.Context, itemID string) (*ProductVariant, error) {
	var variant ProductVariant
	if err := s.db.WithContext(ctx).Where("shopify_inventory_item_id = ?", itemID).First(&variant).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &variant, nil
}

func (s *PostgresStore) UpsertProduct(ctx context.Context, product *Product) error {
	// Ensure ID is not set — let Postgres generate the UUID on insert.
	// If ID were an empty string, GORM would try INSERT with id='' which fails on UUID column.
	product.ID = ""
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "shopify_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"title", "description", "status",
			"handle", "vendor", "product_type", "tags", "body_html",
			"updated_at",
		}),
	}).Create(product).Error
}

func (s *PostgresStore) MarkProductDeletedByShopifyID(ctx context.Context, shopifyID string) error {
	candidates := []string{shopifyID}
	if _, err := strconv.ParseInt(shopifyID, 10, 64); err == nil {
		candidates = append(candidates, "gid://shopify/Product/"+shopifyID)
	} else if idx := strings.LastIndex(shopifyID, "/"); idx >= 0 && idx < len(shopifyID)-1 {
		numericID := shopifyID[idx+1:]
		candidates = append(candidates, numericID)
	}

	return s.db.WithContext(ctx).Model(&Product{}).
		Where("shopify_id IN ?", candidates).
		Updates(map[string]interface{}{"status": "deleted", "updated_at": gorm.Expr("now()")}).
		Error
}

func (s *PostgresStore) UpsertVariant(ctx context.Context, variant *ProductVariant) error {
	// Clear ID so Postgres generates a fresh UUID on insert (avoids empty-string UUID error).
	variant.ID = ""
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "shopify_variant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"title", "sku", "price", "image_src",
			"inventory_quantity", "shopify_inventory_item_id", "weight_kg", "updated_at",
		}),
	}).Create(variant).Error
}

func (s *PostgresStore) UpsertProductImages(ctx context.Context, productID string, images []ProductImage) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Get all current image Shopify IDs
		var currentIDs []string
		for _, img := range images {
			currentIDs = append(currentIDs, img.ShopifyID)
		}

		// Delete images that are no longer present
		if len(currentIDs) > 0 {
			if err := tx.Where("product_id = ? AND shopify_id NOT IN ?", productID, currentIDs).Delete(&ProductImage{}).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Where("product_id = ?", productID).Delete(&ProductImage{}).Error; err != nil {
				return err
			}
		}

		// Upsert the provided images
		if len(images) > 0 {
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "shopify_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"src", "alt", "position"}),
			}).Create(&images).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

func (s *PostgresStore) UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64) error {
	updates := map[string]interface{}{}
	if wsPrice != nil {
		updates["ws_price"] = *wsPrice
	}
	if len(updates) == 0 {
		return nil
	}
	result := s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("shopify_variant_id = ?", variantID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (s *PostgresStore) GetAllVariants(ctx context.Context) ([]ProductVariant, error) {
	var variants []ProductVariant
	if err := s.db.WithContext(ctx).Preload("Product").Find(&variants).Error; err != nil {
		return nil, err
	}
	return variants, nil
}

func (s *PostgresStore) BulkUpdateVariantDimensions(ctx context.Context, inputs []DimensionUpdateInput) error {
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer tx.Rollback()

	for _, input := range inputs {
		res := tx.Model(&ProductVariant{}).Where("shopify_variant_id = ?", input.ShopifyVariantID).Updates(map[string]interface{}{
			"width_cm":  input.WidthCm,
			"height_cm": input.HeightCm,
			"depth_cm":  input.DepthCm,
		})
		if res.Error != nil {
			return res.Error
		}
	}
	return tx.Commit().Error
}

func (s *PostgresStore) GetProductByShopifyID(ctx context.Context, shopifyID string) (*Product, error) {
	var product Product
	err := s.db.WithContext(ctx).Where("shopify_id = ?", shopifyID).First(&product).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &product, nil
}

func (s *PostgresStore) GetProductByID(ctx context.Context, productID string) (*Product, error) {
	var p Product
	err := s.db.WithContext(ctx).Preload("Images").Preload("Variants").Where("id = ?", productID).First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (s *PostgresStore) GetVariantsByProductID(ctx context.Context, productID string) ([]ProductVariant, error) {
	var variants []ProductVariant
	err := s.db.WithContext(ctx).Where("product_id = ?", productID).Find(&variants).Error
	return variants, err
}

func (s *PostgresStore) UpdateProductStatus(ctx context.Context, productID string, fulfillmentType string) error {
	return s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("product_id = ?", productID).
		Updates(map[string]interface{}{"fulfillment_type": fulfillmentType, "updated_at": gorm.Expr("now()")}).Error
}

func (s *PostgresStore) UpdateVariantFulfillmentType(ctx context.Context, variantID string, fulfillmentType string) error {
	return s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("shopify_variant_id = ?", variantID).
		Updates(map[string]interface{}{"fulfillment_type": fulfillmentType, "updated_at": gorm.Expr("now()")}).Error
}

func (s *PostgresStore) UpdateVariantBatchLabel(ctx context.Context, productID string, batchLabel string, expectedShipDate *string) error {
	updates := map[string]interface{}{
		"preorder_batch_label": batchLabel,
		"updated_at":           gorm.Expr("now()"),
	}
	if expectedShipDate != nil && *expectedShipDate != "" {
		if t, err := time.Parse(time.RFC3339, *expectedShipDate); err == nil {
			updates["expected_ship_date"] = t
		} else if t, err := time.Parse("2006-01-02", *expectedShipDate); err == nil {
			updates["expected_ship_date"] = t
		} else {
			updates["expected_ship_date"] = *expectedShipDate // fallback
		}
	}
	return s.db.WithContext(ctx).Model(&ProductVariant{}).
		Where("product_id = ?", productID).
		Updates(updates).Error
}
