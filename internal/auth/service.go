package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/config"
	"github.com/tumbuhindigi-sys/momiji-home-backend/internal/shared/apierror"
	"golang.org/x/crypto/bcrypt"
)

type AuthService interface {
	// Register is only reachable when ENABLE_PUBLIC_REGISTER is true.
	Register(ctx context.Context, req RegisterRequest) (*TokenResponse, error)
	CreateAdmin(ctx context.Context, req CreateAdminRequest) (*UserResponse, error)
	Login(ctx context.Context, req LoginRequest) (*TokenResponse, error)
	Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error)
	GetMe(ctx context.Context, userID string) (*UserResponse, error)
}

type service struct {
	store AuthStore
	cfg   config.AuthConfig
}

func NewAuthService(store AuthStore, cfg config.AuthConfig) AuthService {
	return &service{store: store, cfg: cfg}
}

// Register creates an admin account for local/staging when the public route is enabled.
func (s *service) Register(ctx context.Context, req RegisterRequest) (*TokenResponse, error) {
	user, err := s.createAdminUser(ctx, req.Email, req.Password)
	if err != nil {
		return nil, err
	}
	return s.generateTokens(user)
}

// CreateAdmin creates an admin account without issuing session tokens.
func (s *service) CreateAdmin(ctx context.Context, req CreateAdminRequest) (*UserResponse, error) {
	user, err := s.createAdminUser(ctx, req.Email, req.Password)
	if err != nil {
		return nil, err
	}
	return &UserResponse{
		ID:                user.ID,
		Email:             user.Email,
		Role:              user.Role,
		ShopifyCustomerID: user.ShopifyCustomerID,
	}, nil
}

func (s *service) createAdminUser(ctx context.Context, email, password string) (*User, error) {
	existing, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if existing != nil {
		return nil, apierror.New(http.StatusConflict, "conflict", "Email already registered")
	}

	cost := s.cfg.JWT.BcryptCost
	if cost < 10 {
		cost = 12
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return nil, apierror.ErrInternal
	}

	user := &User{
		Email:        email,
		PasswordHash: string(hashed),
		Role:         "admin",
	}

	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, apierror.ErrInternal
	}

	return user, nil
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

func (s *service) Login(ctx context.Context, req LoginRequest) (*TokenResponse, error) {
	user, err := s.store.GetUserByEmail(ctx, req.Email)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if user == nil {
		return nil, apierror.New(http.StatusUnauthorized, "unauthorized", "Invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, apierror.New(http.StatusUnauthorized, "unauthorized", "Invalid email or password")
	}

	return s.generateTokens(user)
}

func (s *service) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	token, err := jwt.Parse(refreshToken, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, apierror.ErrUnauthorized
		}
		return []byte(s.cfg.RefreshSecret), nil
	})

	if err != nil || !token.Valid {
		return nil, apierror.New(http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "Invalid or expired refresh token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, apierror.ErrUnauthorized
	}

	userID, ok := claims["sub"].(string)
	if !ok {
		return nil, apierror.ErrUnauthorized
	}

	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return nil, apierror.ErrInternal
	}
	if user == nil {
		return nil, apierror.ErrUnauthorized
	}

	return s.generateTokens(user)
}

func (s *service) generateTokens(user *User) (*TokenResponse, error) {
	accessDuration, _ := time.ParseDuration(s.cfg.JWT.AccessTokenTTL)
	refreshDuration, _ := time.ParseDuration(s.cfg.JWT.RefreshTokenTTL)

	accessClaims := jwt.MapClaims{
		"sub":  user.ID,
		"role": user.Role,
		"exp":  time.Now().Add(accessDuration).Unix(),
		"iat":  time.Now().Unix(),
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString([]byte(s.cfg.Secret))
	if err != nil {
		return nil, apierror.ErrInternal
	}

	refreshClaims := jwt.MapClaims{
		"sub": user.ID,
		"exp": time.Now().Add(refreshDuration).Unix(),
		"iat": time.Now().Unix(),
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(s.cfg.RefreshSecret))
	if err != nil {
		return nil, apierror.ErrInternal
	}

	return &TokenResponse{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresIn:    int(accessDuration.Seconds()),
		User: UserResponse{
			ID:                user.ID,
			Email:             user.Email,
			Role:              user.Role,
			ShopifyCustomerID: user.ShopifyCustomerID,
		},
	}, nil
}
