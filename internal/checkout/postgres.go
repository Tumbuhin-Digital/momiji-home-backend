package checkout

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type PostgresStockLockStore struct {
	db *gorm.DB
}

func NewPostgresStockLockStore(db *gorm.DB) StockLockStore {
	return &PostgresStockLockStore{db: db}
}

func (s *PostgresStockLockStore) GetActiveLocksForVariant(ctx context.Context, shopifyVariantID string) (int, error) {
	var totalQty int64
	err := s.db.WithContext(ctx).
		Model(&StockLock{}).
		Where("shopify_variant_id = ? AND expires_at > ?", shopifyVariantID, time.Now()).
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&totalQty).Error
	return int(totalQty), err
}

func (s *PostgresStockLockStore) CreateLocks(ctx context.Context, locks []StockLock) error {
	if len(locks) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Create(&locks).Error
}

func (s *PostgresStockLockStore) DeleteLocksBySession(ctx context.Context, userID, sessionID *string) error {
	query := s.db.WithContext(ctx).Where("1=1")
	hasCond := false

	if userID != nil && *userID != "" {
		query = query.Or("user_id = ?", *userID)
		hasCond = true
	}
	if sessionID != nil && *sessionID != "" {
		query = query.Or("session_id = ?", *sessionID)
		hasCond = true
	}

	if !hasCond {
		return nil
	}

	return query.Delete(&StockLock{}).Error
}

func (s *PostgresStockLockStore) DeleteExpiredLocks(ctx context.Context) error {
	return s.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&StockLock{}).Error
}
