package auth

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type PostgresAuthStore struct {
	db *gorm.DB
}

func NewPostgresAuthStore(db *gorm.DB) AuthStore {
	return &PostgresAuthStore{db: db}
}

func (s *PostgresAuthStore) CreateUser(ctx context.Context, user *User) error {
	return s.db.WithContext(ctx).Create(user).Error
}

func (s *PostgresAuthStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil // Return nil, nil for not found to handle explicitly
		}
		return nil, err
	}
	return &user, nil
}

func (s *PostgresAuthStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	var user User
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}
