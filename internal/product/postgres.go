package product

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) GetVariants(ctx context.Context) ([]ProductVariant, error) {
	var variants []ProductVariant
	err := s.db.WithContext(ctx).Find(&variants).Error
	return variants, err
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

func (s *PostgresStore) UpsertProduct(ctx context.Context, product *Product) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "shopify_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "description", "status", "updated_at"}),
	}).Create(product).Error
}

func (s *PostgresStore) UpsertVariant(ctx context.Context, variant *ProductVariant) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "shopify_variant_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "sku", "price", "image_src", "updated_at"}),
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
	return s.db.WithContext(ctx).Model(&ProductVariant{}).Where("id = ?", variantID).Updates(updates).Error
}
