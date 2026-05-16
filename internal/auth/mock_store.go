package auth

import (
	"context"
	"errors"
)

type MockAuthStore struct {
	Users map[string]*User // by Email
	ByID  map[string]*User // by ID
}

func NewMockAuthStore() *MockAuthStore {
	return &MockAuthStore{
		Users: make(map[string]*User),
		ByID:  make(map[string]*User),
	}
}

func (m *MockAuthStore) CreateUser(ctx context.Context, user *User) error {
	if _, exists := m.Users[user.Email]; exists {
		return errors.New("unique constraint violation")
	}
	if user.ID == "" {
		user.ID = "mock-uuid"
	}
	m.Users[user.Email] = user
	m.ByID[user.ID] = user
	return nil
}

func (m *MockAuthStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	if user, exists := m.Users[email]; exists {
		return user, nil
	}
	return nil, nil
}

func (m *MockAuthStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	if user, exists := m.ByID[id]; exists {
		return user, nil
	}
	return nil, nil
}
