package security

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"user-management/internal/domain"
)

const testSecret = "0123456789abcdef0123456789abcdef" // 32 bytes

func newTestManager(t *testing.T) *JWTManager {
	t.Helper()
	m, err := NewJWTManager(testSecret, "test-issuer", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestJWT_RoundTrip(t *testing.T) {
	m := newTestManager(t)
	tok, exp, err := m.Issue(&domain.User{ID: "u1", Email: "a@b.io"})
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(exp) < 59*time.Minute {
		t.Errorf("expiry too soon: %v", exp)
	}
	c, err := m.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.UserID != "u1" || c.Email != "a@b.io" {
		t.Errorf("claims = %+v", c)
	}
	// Header must declare HS256.
	parsed, _, _ := jwt.NewParser().ParseUnverified(tok, &jwt.RegisteredClaims{})
	if parsed.Method.Alg() != "HS256" {
		t.Errorf("alg = %s, want HS256", parsed.Method.Alg())
	}
}

func TestJWT_RejectsInvalidTokens(t *testing.T) {
	m := newTestManager(t)
	valid, _, _ := m.Issue(&domain.User{ID: "u1", Email: "a@b.io"})

	sign := func(method jwt.SigningMethod, key any, claims jwt.Claims) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	goodClaims := func() userClaims {
		now := time.Now()
		return userClaims{RegisteredClaims: jwt.RegisteredClaims{
			Subject: "u1", Issuer: "test-issuer",
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}}
	}

	parts := strings.Split(valid, ".")
	tampered := parts[0] + "." + parts[1] + "x." + parts[2]

	noExp := goodClaims()
	noExp.ExpiresAt = nil
	wrongIss := goodClaims()
	wrongIss.Issuer = "someone-else"
	noSub := goodClaims()
	noSub.Subject = ""

	tests := map[string]string{
		"empty":            "",
		"garbage":          "not.a.jwt",
		"tampered payload": tampered,
		"wrong secret":     sign(jwt.SigningMethodHS256, []byte("another-secret-another-secret-xx"), goodClaims()),
		"alg none":         sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, goodClaims()),
		"alg HS512":        sign(jwt.SigningMethodHS512, []byte(testSecret), goodClaims()),
		"missing exp":      sign(jwt.SigningMethodHS256, []byte(testSecret), noExp),
		"wrong issuer":     sign(jwt.SigningMethodHS256, []byte(testSecret), wrongIss),
		"missing subject":  sign(jwt.SigningMethodHS256, []byte(testSecret), noSub),
	}
	for name, tok := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := m.Verify(tok); !errors.Is(err, ErrInvalidToken) {
				t.Errorf("Verify err = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestJWT_Expired(t *testing.T) {
	m := newTestManager(t)
	tok, _, _ := m.Issue(&domain.User{ID: "u1"})
	m.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	if _, err := m.Verify(tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired token accepted: %v", err)
	}
}

func TestNewJWTManager_Validation(t *testing.T) {
	if _, err := NewJWTManager("too-short", "iss", time.Hour); err == nil {
		t.Error("short secret accepted")
	}
	if _, err := NewJWTManager(testSecret, "iss", 0); err == nil {
		t.Error("zero ttl accepted")
	}
}

func TestBcryptHasher(t *testing.T) {
	h := NewBcryptHasher(bcrypt.MinCost)
	hash, err := h.Hash("password123")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "password123" || !strings.HasPrefix(hash, "$2") {
		t.Fatalf("not a bcrypt hash: %q", hash)
	}
	if err := h.Compare(hash, "password123"); err != nil {
		t.Errorf("correct password rejected: %v", err)
	}
	if err := h.Compare(hash, "wrong"); err == nil {
		t.Error("wrong password accepted")
	}
}
