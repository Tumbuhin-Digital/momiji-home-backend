package preordercustomtext

import (
	"context"
	"time"
)

type PreorderCustomText struct {
	ID        string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Label     string     `gorm:"not null"`
	DeletedAt *time.Time `gorm:"index"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (PreorderCustomText) TableName() string {
	return "preorder_custom_texts"
}

type Store interface {
	List(ctx context.Context, search string) ([]PreorderCustomText, error)
	Create(ctx context.Context, label string) (*PreorderCustomText, error)
	GetByID(ctx context.Context, id string) (*PreorderCustomText, error)
	GetByLabel(ctx context.Context, label string) (*PreorderCustomText, error)
	SoftDelete(ctx context.Context, id string) error
	CountVariantUsage(ctx context.Context, label string) (int64, error)
	EnsureByLabel(ctx context.Context, label string) (*PreorderCustomText, error)
}
