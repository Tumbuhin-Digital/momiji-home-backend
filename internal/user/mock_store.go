package user

import (
	"context"
)

type MockUserStore struct {
	ByID map[string]*User
}

func NewMockUserStore() *MockUserStore {
	return &MockUserStore{
		ByID: make(map[string]*User),
	}
}

func (m *MockUserStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	if user, exists := m.ByID[id]; exists {
		return user, nil
	}
	return nil, nil
}

func (m *MockUserStore) UpdateUser(ctx context.Context, id string, data map[string]interface{}) error {
	user, exists := m.ByID[id]
	if !exists {
		return nil // mimicking gorm model updates which don't error on 0 rows
	}

	if shopifyID, ok := data["shopify_customer_id"].(string); ok {
		user.ShopifyCustomerID = shopifyID
	}
	
	m.ByID[id] = user
	return nil
}
