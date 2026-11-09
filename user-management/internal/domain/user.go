// Package domain contains the core business entities and rules.
// It has no dependencies on frameworks, drivers or transport layers.
package domain

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Validation limits.
const (
	MinNameLength     = 1
	MaxNameLength     = 100
	MaxEmailLength    = 254 // RFC 5321
	MinPasswordLength = 8
	MaxPasswordBytes  = 72 // bcrypt silently truncates beyond 72 bytes
)

// User is the core user entity.
type User struct {
	ID           string
	Name         string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// NormalizeEmail trims surrounding whitespace and lower-cases an email so that
// uniqueness checks are case-insensitive.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormalizeName trims surrounding whitespace from a name.
func NormalizeName(name string) string {
	return strings.TrimSpace(name)
}

// ValidateName checks a (normalized) name.
func ValidateName(name string) string {
	n := utf8.RuneCountInString(name)
	switch {
	case n < MinNameLength:
		return "is required"
	case n > MaxNameLength:
		return "must be at most 100 characters"
	}
	return ""
}

// ValidateEmail checks a (normalized) email address.
func ValidateEmail(email string) string {
	if email == "" {
		return "is required"
	}
	if len(email) > MaxEmailLength {
		return "must be at most 254 characters"
	}
	addr, err := mail.ParseAddress(email)
	// mail.ParseAddress accepts "Name <a@b.c>"; require the bare address only.
	if err != nil || addr.Address != email || !strings.Contains(email[strings.LastIndex(email, "@"):], ".") {
		return "must be a valid email address"
	}
	return ""
}

// ValidatePassword checks a plaintext password before hashing.
func ValidatePassword(password string) string {
	switch {
	case password == "":
		return "is required"
	case utf8.RuneCountInString(password) < MinPasswordLength:
		return "must be at least 8 characters"
	case len(password) > MaxPasswordBytes:
		return "must be at most 72 bytes"
	}
	return ""
}
