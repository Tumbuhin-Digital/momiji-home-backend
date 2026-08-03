package checkout

import (
	"context"
	"time"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/uszip"
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
	query := s.db.WithContext(ctx)
	hasCond := false

	if userID != nil && *userID != "" {
		query = query.Where("user_id = ?", *userID)
		hasCond = true
	}
	if sessionID != nil && *sessionID != "" {
		if hasCond {
			query = query.Or("session_id = ?", *sessionID)
		} else {
			query = query.Where("session_id = ?", *sessionID)
		}
		hasCond = true
	}

	if !hasCond {
		return nil
	}

	return query.Delete(&StockLock{}).Error
}

func (s *PostgresStockLockStore) DeleteLocksByCheckoutReference(
	ctx context.Context,
	checkoutReference string,
	userID, sessionID *string,
) error {
	if checkoutReference == "" {
		return nil
	}

	query := s.db.WithContext(ctx).Where("checkout_reference = ?", checkoutReference)

	if userID != nil && *userID != "" && sessionID != nil && *sessionID != "" {
		query = query.Where("user_id = ? OR session_id = ?", *userID, *sessionID)
	} else if userID != nil && *userID != "" {
		query = query.Where("user_id = ?", *userID)
	} else if sessionID != nil && *sessionID != "" {
		query = query.Where("session_id = ?", *sessionID)
	}

	return query.Delete(&StockLock{}).Error
}

func (s *PostgresStockLockStore) DeleteExpiredLocks(ctx context.Context) error {
	return s.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&StockLock{}).Error
}

func (s *PostgresStockLockStore) GetUSZipCodeDetails(ctx context.Context, zip string) (*UsZipCode, error) {
	normalized, ok := uszip.NormalizeUSZip(zip)
	if !ok {
		return nil, nil
	}

	var zipCode UsZipCode
	if err := s.db.WithContext(ctx).Where("zip_code = ?", normalized).First(&zipCode).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &zipCode, nil
}

func (s *PostgresStockLockStore) UpsertCheckoutSession(ctx context.Context, session *CheckoutSession) error {
	if session == nil || session.CheckoutReference == "" {
		return nil
	}
	now := time.Now()
	session.UpdatedAt = now
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	return s.db.WithContext(ctx).Save(session).Error
}

func (s *PostgresStockLockStore) GetCheckoutSession(ctx context.Context, checkoutReference string) (*CheckoutSession, error) {
	if checkoutReference == "" {
		return nil, nil
	}
	var session CheckoutSession
	err := s.db.WithContext(ctx).Where("checkout_reference = ?", checkoutReference).First(&session).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &session, nil
}

func (s *PostgresStockLockStore) DeleteCheckoutSession(ctx context.Context, checkoutReference string) error {
	if checkoutReference == "" {
		return nil
	}
	return s.db.WithContext(ctx).Where("checkout_reference = ?", checkoutReference).Delete(&CheckoutSession{}).Error
}

func (s *PostgresStockLockStore) ListExpiredCheckoutSessions(ctx context.Context, asOf time.Time) ([]CheckoutSession, error) {
	var sessions []CheckoutSession
	err := s.db.WithContext(ctx).Where("expires_at <= ?", asOf).Find(&sessions).Error
	return sessions, err
}

func (s *PostgresStockLockStore) DeleteExpiredCheckoutSessions(ctx context.Context, asOf time.Time) error {
	return s.db.WithContext(ctx).Where("expires_at <= ?", asOf).Delete(&CheckoutSession{}).Error
}
