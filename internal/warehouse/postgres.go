package warehouse

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) List(ctx context.Context) ([]Warehouse, error) {
	var rows []Warehouse
	err := s.db.WithContext(ctx).
		Order("is_default DESC, code ASC").
		Find(&rows).Error
	return rows, err
}

func (s *PostgresStore) GetByCode(ctx context.Context, code string) (*Warehouse, error) {
	var row Warehouse
	err := s.db.WithContext(ctx).Where("code = ?", code).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *PostgresStore) UpdateByCode(ctx context.Context, code string, updates map[string]interface{}) (*Warehouse, error) {
	updates["updated_at"] = time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&Warehouse{}).Where("code = ?", code).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return s.GetByCode(ctx, code)
}
