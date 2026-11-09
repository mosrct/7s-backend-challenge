// Package application implements the use cases of the service. It orchestrates
// domain rules and talks to infrastructure exclusively through ports.
package application

import (
	"context"
	"fmt"
	"time"

	"user-management/internal/domain"
	"user-management/internal/port"
)

// Pagination defaults for listing users.
const (
	DefaultListLimit int64 = 50
	MaxListLimit     int64 = 100
)

// CreateUserInput is the input for creating/registering a user.
type CreateUserInput struct {
	Name     string
	Email    string
	Password string
}

// UpdateUserInput holds optional changes; nil fields are left untouched.
type UpdateUserInput struct {
	Name  *string
	Email *string
}

// ListUsersInput controls pagination. Zero Limit means DefaultListLimit.
type ListUsersInput struct {
	Offset int64
	Limit  int64
}

// UserPage is one page of users plus the total number of users.
type UserPage struct {
	Users  []domain.User
	Total  int64
	Offset int64
	Limit  int64
}

// UserService implements user CRUD use cases.
type UserService struct {
	repo   port.UserRepository
	hasher port.PasswordHasher
	now    func() time.Time
}

// NewUserService builds a UserService. now may be nil (defaults to time.Now).
func NewUserService(repo port.UserRepository, hasher port.PasswordHasher, now func() time.Time) *UserService {
	if now == nil {
		now = time.Now
	}
	return &UserService{repo: repo, hasher: hasher, now: now}
}

// Create validates input, hashes the password and persists a new user.
func (s *UserService) Create(ctx context.Context, in CreateUserInput) (*domain.User, error) {
	name := domain.NormalizeName(in.Name)
	email := domain.NormalizeEmail(in.Email)

	verr := domain.NewValidationError()
	verr.Add("name", domain.ValidateName(name))
	verr.Add("email", domain.ValidateEmail(email))
	verr.Add("password", domain.ValidatePassword(in.Password))
	if err := verr.OrNil(); err != nil {
		return nil, err
	}

	hash, err := s.hasher.Hash(in.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &domain.User{
		Name:         name,
		Email:        email,
		PasswordHash: hash,
		// MongoDB stores milliseconds; truncate so the returned value matches
		// what a subsequent read would return.
		CreatedAt: s.now().UTC().Truncate(time.Millisecond),
	}
	// Email uniqueness is enforced by the repository (unique index), which is
	// race-free unlike a read-then-write check here.
	if err := s.repo.Create(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// Get returns a user by ID.
func (s *UserService) Get(ctx context.Context, id string) (*domain.User, error) {
	return s.repo.GetByID(ctx, id)
}

// List returns a page of users.
func (s *UserService) List(ctx context.Context, in ListUsersInput) (*UserPage, error) {
	verr := domain.NewValidationError()
	if in.Offset < 0 {
		verr.Add("offset", "must be >= 0")
	}
	if in.Limit < 0 || in.Limit > MaxListLimit {
		verr.Add("limit", fmt.Sprintf("must be between 1 and %d", MaxListLimit))
	}
	if err := verr.OrNil(); err != nil {
		return nil, err
	}
	if in.Limit == 0 {
		in.Limit = DefaultListLimit
	}

	users, err := s.repo.List(ctx, in.Offset, in.Limit)
	if err != nil {
		return nil, err
	}
	total, err := s.repo.Count(ctx)
	if err != nil {
		return nil, err
	}
	return &UserPage{Users: users, Total: total, Offset: in.Offset, Limit: in.Limit}, nil
}

// Update changes a user's name and/or email. Only the user themself
// (actorID == id) may update their account.
func (s *UserService) Update(ctx context.Context, actorID, id string, in UpdateUserInput) (*domain.User, error) {
	if actorID != id {
		return nil, domain.ErrForbidden
	}

	verr := domain.NewValidationError()
	var changes port.UserChanges
	if in.Name == nil && in.Email == nil {
		verr.Add("body", "at least one of name or email must be provided")
	}
	if in.Name != nil {
		name := domain.NormalizeName(*in.Name)
		verr.Add("name", domain.ValidateName(name))
		changes.Name = &name
	}
	if in.Email != nil {
		email := domain.NormalizeEmail(*in.Email)
		verr.Add("email", domain.ValidateEmail(email))
		changes.Email = &email
	}
	if err := verr.OrNil(); err != nil {
		return nil, err
	}

	return s.repo.Update(ctx, id, changes)
}

// Delete removes a user. Only the user themself may delete their account.
func (s *UserService) Delete(ctx context.Context, actorID, id string) error {
	if actorID != id {
		return domain.ErrForbidden
	}
	return s.repo.Delete(ctx, id)
}

// Count returns the total number of users.
func (s *UserService) Count(ctx context.Context) (int64, error) {
	return s.repo.Count(ctx)
}
