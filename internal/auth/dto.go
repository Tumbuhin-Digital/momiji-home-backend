package auth

// Removed RegisterRequest as per API contract

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

type TokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int          `json:"expires_in"` // seconds
	User         UserResponse `json:"user"`
}

type UserResponse struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Role              string `json:"role"`
	ShopifyCustomerID string `json:"shopify_customer_id,omitempty"`
}
