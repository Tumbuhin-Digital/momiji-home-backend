package auth

// RegisterRequest is DEV ONLY — remove or gate behind feature flag before production.
// Added temporarily to allow FE development without direct DB access.
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type TokenResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"-"`
	ExpiresIn    int          `json:"expires_in"` // seconds
	User         UserResponse `json:"user"`
}

type UserResponse struct {
	ID                string `json:"id"`
	Email             string `json:"email"`
	Role              string `json:"role"`
	ShopifyCustomerID string `json:"shopify_customer_id,omitempty"`
}
