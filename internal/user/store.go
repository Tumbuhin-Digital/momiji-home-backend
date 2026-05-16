package user

import (
	"context"
	"time"
)

type User struct {
	ID                string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Email             string
	ShopifyCustomerID string
	Role              string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type UserStore interface {
	GetUserByID(ctx context.Context, id string) (*User, error)
	UpdateUser(ctx context.Context, id string, data map[string]interface{}) error
}
