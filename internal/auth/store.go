package auth

import (
	"context"
	"time"
)

type User struct {
	ID                string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Email             string    `gorm:"uniqueIndex;not null"`
	ShopifyCustomerID string
	PasswordHash      string    `gorm:"not null"`
	Role              string    `gorm:"not null;default:'customer'"`
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type AuthStore interface {
	CreateUser(ctx context.Context, user *User) error
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
}
