package settings

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) SettingsStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Get(ctx context.Context, key string) (string, error) {
	var setting Setting
	err := s.db.WithContext(ctx).Where("key = ?", key).First(&setting).Error
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (s *PostgresStore) GetMany(ctx context.Context, keys []string) (map[string]string, error) {
	var settings []Setting
	err := s.db.WithContext(ctx).Where("key IN ?", keys).Find(&settings).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]string, len(settings))
	for _, setting := range settings {
		result[setting.Key] = setting.Value
	}
	return result, nil
}

func (s *PostgresStore) Upsert(ctx context.Context, key, value string) error {
	setting := Setting{
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now().UTC(),
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&setting).Error
}
