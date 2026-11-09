// Package security provides password hashing and token adapters.
package security

import "golang.org/x/crypto/bcrypt"

// BcryptHasher implements port.PasswordHasher using bcrypt.
type BcryptHasher struct {
	cost int
}

// NewBcryptHasher returns a hasher with the given cost. A cost of 0 uses
// bcrypt.DefaultCost. Tests may use bcrypt.MinCost for speed.
func NewBcryptHasher(cost int) *BcryptHasher {
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	return &BcryptHasher{cost: cost}
}

// Hash returns the bcrypt hash of password.
func (h *BcryptHasher) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Compare returns nil if password matches hash.
func (h *BcryptHasher) Compare(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}
