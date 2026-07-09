package auth

// RegisterRequest is only used when ENABLE_PUBLIC_REGISTER=true (local/staging).
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// CreateAdminRequest creates a new admin; requires Auth + RBAC(admin).
type CreateAdminRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type TokenResponse struct {
	AccessToken  string       `json:"-"`
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
