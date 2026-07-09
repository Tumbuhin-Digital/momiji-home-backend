package preordercustomtext

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) List(ctx context.Context, search string) ([]PreorderCustomText, error) {
	var items []PreorderCustomText
	query := s.db.WithContext(ctx).
		Model(&PreorderCustomText{}).
		Where("deleted_at IS NULL")
	if search = strings.TrimSpace(search); search != "" {
		query = query.Where("label ILIKE ?", "%"+search+"%")
	}
	err := query.Order("label ASC").Find(&items).Error
	return items, err
}

func (s *PostgresStore) Create(ctx context.Context, label string) (*PreorderCustomText, error) {
	item := &PreorderCustomText{Label: label}
	if err := s.db.WithContext(ctx).Create(item).Error; err != nil {
		return nil, err
	}
	return item, nil
}

func (s *PostgresStore) GetByID(ctx context.Context, id string) (*PreorderCustomText, error) {
	var item PreorderCustomText
	err := s.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (s *PostgresStore) GetByLabel(ctx context.Context, label string) (*PreorderCustomText, error) {
	var item PreorderCustomText
	err := s.db.WithContext(ctx).
		Where("LOWER(label) = LOWER(?) AND deleted_at IS NULL", label).
		First(&item).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func (s *PostgresStore) SoftDelete(ctx context.Context, id string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).
		Model(&PreorderCustomText{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("deleted_at", now).Error
}

func (s *PostgresStore) CountVariantUsage(ctx context.Context, label string) (int64, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Table("product_variants").
		Where("preorder_batch_label = ?", label).
		Count(&count).Error
	return count, err
}

func (s *PostgresStore) EnsureByLabel(ctx context.Context, label string) (*PreorderCustomText, error) {
	existing, err := s.GetByLabel(ctx, label)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	return s.Create(ctx, label)
}
