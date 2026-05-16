package user

type UserResponse struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Role              string `json:"role"`
	ShopifyCustomerID string `json:"shopify_customer_id,omitempty"`
}

type UpdateUserRequest struct {
	ShopifyCustomerID string `json:"shopify_customer_id" validate:"required"`
}
