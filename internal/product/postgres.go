package product

import (
	"context"
	"errors"
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

	if q.Search != "" {
		searchPattern := "%" + q.Search + "%"
		query = query.Where("title ILIKE ? OR description ILIKE ?", searchPattern, searchPattern)
	}

	if q.FulfillmentType != "" {
		query = query.Joins("JOIN product_variants ON product_variants.product_id = products.id").
			Where("product_variants.fulfillment_type = ?", q.FulfillmentType).
			Group("products.id")
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
	}

	err := query.Preload("Variants").Order(orderStr).Offset(offset).Limit(limit).Find(&products).Error
	return products, total, err
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
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "shopify_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "description", "status", "updated_at"}),
	}).Create(product).Error
}

func (s *PostgresStore) UpsertVariant(ctx context.Context, variant *ProductVariant) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "shopify_variant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "sku", "price", "image_src", "inventory_quantity", "fulfillment_type", "shopify_inventory_item_id", "updated_at"}),
	}).Create(variant).Error
}

func (s *PostgresStore) UpdateVariantPrices(ctx context.Context, variantID string, wsPrice *float64, retailPrice *float64) error {
	updates := map[string]interface{}{}
	if wsPrice != nil {
		updates["ws_price"] = *wsPrice
	}
	if retailPrice != nil {
		updates["retail_price"] = *retailPrice
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
	err := s.db.WithContext(ctx).Where("id = ?", productID).First(&p).Error
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
