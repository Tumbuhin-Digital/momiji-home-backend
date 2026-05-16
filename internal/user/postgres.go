package user

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

type PostgresUserStore struct {
	db *gorm.DB
}

func NewPostgresUserStore(db *gorm.DB) UserStore {
	return &PostgresUserStore{db: db}
}

func (s *PostgresUserStore) GetUserByID(ctx context.Context, id string) (*User, error) {
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

func (s *PostgresUserStore) UpdateUser(ctx context.Context, id string, data map[string]interface{}) error {
	return s.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Updates(data).Error
}
