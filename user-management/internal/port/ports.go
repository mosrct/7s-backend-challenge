// Package port defines the interfaces (ports) through which the application
// core talks to the outside world. Adapters in internal/adapter implement them.
package port

import (
	"context"
	"time"

	"user-management/internal/domain"
)

// UserRepository is the outbound persistence port for users.
//
// Implementations must:
//   - assign User.ID on Create,
//   - return domain.ErrUserNotFound when an ID does not exist (including IDs
//     that are syntactically invalid for the backing store),
//   - return domain.ErrEmailTaken when a write violates email uniqueness.
type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	GetByID(ctx context.Context, id string) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	List(ctx context.Context, offset, limit int64) ([]domain.User, error)
	Update(ctx context.Context, id string, changes UserChanges) (*domain.User, error)
	Delete(ctx context.Context, id string) error
	Count(ctx context.Context) (int64, error)
}

// UserChanges holds the fields to update. Nil means "leave unchanged".
type UserChanges struct {
	Name  *string
	Email *string
}

// PasswordHasher hashes and verifies passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	// Compare returns nil if password matches hash.
	Compare(hash, password string) error
}

// Claims are the authenticated identity extracted from a verified token.
type Claims struct {
	UserID    string
	Email     string
	ExpiresAt time.Time
}

// TokenManager issues and verifies access tokens.
type TokenManager interface {
	Issue(user *domain.User) (token string, expiresAt time.Time, err error)
	Verify(token string) (*Claims, error)
}
