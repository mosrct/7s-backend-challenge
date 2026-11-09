package domain

import (
	"strings"
	"testing"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"alice@example.com", true},
		{"a.b+tag@sub.example.co", true},
		{"", false},
		{"alice", false},
		{"alice@", false},
		{"@example.com", false},
		{"alice@localhost", false},
		{"Alice <alice@example.com>", false},
		{"alice@example.com ", false}, // not normalized
		{strings.Repeat("a", 250) + "@x.io", false},
	}
	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := ValidateEmail(tt.email) == ""
			if got != tt.valid {
				t.Errorf("ValidateEmail(%q) valid=%v, want %v", tt.email, got, tt.valid)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := map[string]bool{
		"Alice":                  true,
		"":                       false,
		strings.Repeat("x", 100): true,
		strings.Repeat("x", 101): false,
		strings.Repeat("ก", 100): true, // counts runes, not bytes
	}
	for name, valid := range tests {
		if got := ValidateName(name) == ""; got != valid {
			t.Errorf("ValidateName(len=%d) valid=%v, want %v", len(name), got, valid)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	tests := map[string]bool{
		"":                      false,
		"short":                 false,
		"longenough":            true,
		strings.Repeat("p", 72): true,
		strings.Repeat("p", 73): false, // bcrypt limit
	}
	for pw, valid := range tests {
		if got := ValidatePassword(pw) == ""; got != valid {
			t.Errorf("ValidatePassword(len=%d) valid=%v, want %v", len(pw), got, valid)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  Alice@Example.COM "); got != "alice@example.com" {
		t.Errorf("got %q", got)
	}
}

func TestValidationError(t *testing.T) {
	v := NewValidationError()
	if v.OrNil() != nil {
		t.Fatal("empty ValidationError must be nil error")
	}
	v.Add("name", "")
	if v.OrNil() != nil {
		t.Fatal("Add with empty message must be ignored")
	}
	v.Add("name", "is required")
	v.Add("email", "is invalid")
	if v.OrNil() == nil {
		t.Fatal("expected error")
	}
	if want := "validation failed: email is invalid; name is required"; v.Error() != want {
		t.Errorf("Error() = %q, want %q", v.Error(), want)
	}
}
