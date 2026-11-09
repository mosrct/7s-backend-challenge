package security

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"user-management/internal/domain"
	"user-management/internal/port"
)

// MinSecretLength is the minimum HMAC key size. RFC 7518 §3.2 requires a key
// at least as long as the hash output (256 bits for HS256).
const MinSecretLength = 32

// ErrInvalidToken is returned for any token that fails verification.
var ErrInvalidToken = errors.New("invalid token")

type userClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// JWTManager implements port.TokenManager with HMAC-SHA256 signed JWTs.
type JWTManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

// NewJWTManager validates its configuration and returns a JWTManager.
func NewJWTManager(secret, issuer string, ttl time.Duration) (*JWTManager, error) {
	if len(secret) < MinSecretLength {
		return nil, fmt.Errorf("jwt secret must be at least %d bytes", MinSecretLength)
	}
	if ttl <= 0 {
		return nil, errors.New("jwt ttl must be positive")
	}
	return &JWTManager{secret: []byte(secret), issuer: issuer, ttl: ttl, now: time.Now}, nil
}

// Issue signs a token for user.
func (m *JWTManager) Issue(user *domain.User) (string, time.Time, error) {
	now := m.now()
	exp := now.Add(m.ttl)
	claims := userClaims{
		Email: user.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp.Truncate(time.Second), nil
}

// Verify parses and validates a token: signature, algorithm (HS256 only, which
// blocks "alg":"none" and algorithm-confusion attacks), issuer, expiry and
// not-before.
func (m *JWTManager) Verify(token string) (*port.Claims, error) {
	var claims userClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}
	return &port.Claims{
		UserID:    claims.Subject,
		Email:     claims.Email,
		ExpiresAt: claims.ExpiresAt.Time,
	}, nil
}
