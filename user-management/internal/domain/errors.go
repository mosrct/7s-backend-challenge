package domain

import (
	"errors"
	"sort"
	"strings"
)

// Sentinel errors returned by the application layer. Transport adapters map
// them to protocol-specific status codes.
var (
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailTaken         = errors.New("email already in use")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrForbidden          = errors.New("operation not permitted")
	ErrUnauthenticated    = errors.New("authentication required")
)

// ValidationError describes one or more invalid input fields.
type ValidationError struct {
	Fields map[string]string
}

// NewValidationError returns an empty ValidationError ready to collect fields.
func NewValidationError() *ValidationError {
	return &ValidationError{Fields: map[string]string{}}
}

// Add records a problem for field if msg is non-empty.
func (e *ValidationError) Add(field, msg string) {
	if msg != "" {
		e.Fields[field] = msg
	}
}

// OrNil returns e if it holds any field errors, otherwise nil. This avoids the
// classic typed-nil-in-interface pitfall.
func (e *ValidationError) OrNil() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+" "+e.Fields[k])
	}
	return "validation failed: " + strings.Join(parts, "; ")
}
