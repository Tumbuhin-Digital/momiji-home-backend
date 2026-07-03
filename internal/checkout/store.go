package checkout

import (
	"context"
	"time"
)

type StockLock struct {
	ID                string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	ShopifyVariantID  string
	Quantity          int
	SessionID         *string
	UserID            *string
	CheckoutReference *string
	ExpiresAt         time.Time
	CreatedAt         time.Time
}

type UsZipCode struct {
	ZipCode   string `gorm:"primaryKey"`
	City      string
	StateAbbr string
	StateName string
}

func (UsZipCode) TableName() string {
	return "us_zip_codes"
}

type StockLockStore interface {
	GetActiveLocksForVariant(ctx context.Context, shopifyVariantID string) (int, error)
	CreateLocks(ctx context.Context, locks []StockLock) error
	DeleteLocksBySession(ctx context.Context, userID, sessionID *string) error
	DeleteLocksByCheckoutReference(ctx context.Context, checkoutReference string, userID, sessionID *string) error
	DeleteExpiredLocks(ctx context.Context) error
	GetUSZipCodeDetails(ctx context.Context, zip string) (*UsZipCode, error)
}
