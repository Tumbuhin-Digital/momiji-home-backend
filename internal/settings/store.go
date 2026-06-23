package settings

import (
	"context"
	"time"
)

const (
	KeyCheckoutDueNowNote   = "checkout_due_now_note"
	KeyCheckoutDueLaterNote = "checkout_due_later_note"
)

type Setting struct {
	Key       string    `gorm:"column:key;primaryKey"`
	Value     string    `gorm:"column:value"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Setting) TableName() string {
	return "app_settings"
}

type SettingsStore interface {
	Get(ctx context.Context, key string) (string, error)
	GetMany(ctx context.Context, keys []string) (map[string]string, error)
	Upsert(ctx context.Context, key, value string) error
}
