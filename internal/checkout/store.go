package checkout

import (
	"context"
	"time"
)

type StockLock struct {
	ID               string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ShopifyVariantID string
	Quantity         int
	SessionID        *string
	UserID           *string
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

type StockLockStore interface {
	GetActiveLocksForVariant(ctx context.Context, shopifyVariantID string) (int, error)
	CreateLocks(ctx context.Context, locks []StockLock) error
	DeleteLocksBySession(ctx context.Context, userID, sessionID *string) error
	DeleteExpiredLocks(ctx context.Context) error
}
