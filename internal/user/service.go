package user

import (
	"context"

	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
)

type UserService interface {
	GetMe(ctx context.Context, userID string) (*UserResponse, error)
	UpdateMe(ctx context.Context, userID string, req UpdateUserRequest) (*UserResponse, error)
}

type service struct {
	store UserStore
}

func NewUserService(store UserStore) UserService {
	return &service{store: store}
}

func (s *service) GetMe(ctx context.Context, userID string) (*UserResponse, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if user == nil {
		return nil, apierror.ErrNotFound
	}

	return &UserResponse{
		ID:                user.ID,
		Email:             user.Email,
		Role:              user.Role,
		ShopifyCustomerID: user.ShopifyCustomerID,
	}, nil
}

func (s *service) UpdateMe(ctx context.Context, userID string, req UpdateUserRequest) (*UserResponse, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if user == nil {
		return nil, apierror.ErrNotFound
	}

	data := map[string]interface{}{
		"shopify_customer_id": req.ShopifyCustomerID,
	}

	if err := s.store.UpdateUser(ctx, userID, data); err != nil {
		return nil, apierror.ErrInternal
	}

	// Fetch updated
	updatedUser, err := s.store.GetUserByID(ctx, userID)
	if err != nil || updatedUser == nil {
		return nil, apierror.ErrInternal
	}

	return &UserResponse{
		ID:                updatedUser.ID,
		Email:             updatedUser.Email,
		Role:              updatedUser.Role,
		ShopifyCustomerID: updatedUser.ShopifyCustomerID,
	}, nil
}
