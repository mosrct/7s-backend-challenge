package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"user-management/internal/domain"
	"user-management/internal/port"
)

// LoginInput holds login credentials.
type LoginInput struct {
	Email    string
	Password string
}

// LoginResult is returned on successful authentication.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	User      *domain.User
}

// AuthService implements registration and login.
type AuthService struct {
	users  *UserService
	repo   port.UserRepository
	hasher port.PasswordHasher
	tokens port.TokenManager

	// dummyHash is compared against when the email is unknown so that the
	// response time does not reveal whether an account exists.
	dummyHash string
}

// NewAuthService builds an AuthService.
func NewAuthService(users *UserService, repo port.UserRepository, hasher port.PasswordHasher, tokens port.TokenManager) (*AuthService, error) {
	dummy, err := hasher.Hash("timing-equalisation-dummy-password")
	if err != nil {
		return nil, fmt.Errorf("prepare dummy hash: %w", err)
	}
	return &AuthService{users: users, repo: repo, hasher: hasher, tokens: tokens, dummyHash: dummy}, nil
}

// Register creates a new account. It is the public counterpart of
// UserService.Create.
func (s *AuthService) Register(ctx context.Context, in CreateUserInput) (*domain.User, error) {
	return s.users.Create(ctx, in)
}

// Login verifies credentials and issues a signed JWT.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	email := domain.NormalizeEmail(in.Email)

	verr := domain.NewValidationError()
	if email == "" {
		verr.Add("email", "is required")
	}
	if in.Password == "" {
		verr.Add("password", "is required")
	}
	if err := verr.OrNil(); err != nil {
		return nil, err
	}

	user, err := s.repo.GetByEmail(ctx, email)
	if errors.Is(err, domain.ErrUserNotFound) {
		_ = s.hasher.Compare(s.dummyHash, in.Password) // equalise timing
		return nil, domain.ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if err := s.hasher.Compare(user.PasswordHash, in.Password); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	token, exp, err := s.tokens.Issue(user)
	if err != nil {
		return nil, fmt.Errorf("issue token: %w", err)
	}
	return &LoginResult{Token: token, ExpiresAt: exp, User: user}, nil
}

// Authenticate verifies a raw bearer token and returns its claims.
func (s *AuthService) Authenticate(token string) (*port.Claims, error) {
	if token == "" {
		return nil, domain.ErrUnauthenticated
	}
	claims, err := s.tokens.Verify(token)
	if err != nil {
		return nil, domain.ErrUnauthenticated
	}
	return claims, nil
}
