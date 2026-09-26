package util

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Token types carried in the "typ" claim so an access token can never be
// presented where a reset token is expected and vice versa.
const (
	TokenTypeAccess = "access"
	TokenTypeReset  = "reset"
)

// TokenClaims is the HS256 claim set: sub = user id, jti, exp, iat, typ.
type TokenClaims struct {
	Typ string `json:"typ"`
	jwt.RegisteredClaims
}

// ErrInvalidToken is returned for any token that does not verify (bad
// signature, expired, wrong type, malformed).
var ErrInvalidToken = errors.New("invalid token")

// SignToken issues an HS256 token of the given type for subject, valid for ttl
// from now. It returns the token, its jti and its expiry.
func SignToken(secret, typ, subject string, now time.Time, ttl time.Duration) (token, jti string, expiresAt time.Time, err error) {
	if secret == "" {
		return "", "", time.Time{}, errors.New("jwt: empty secret")
	}
	jti = uuid.NewString()
	expiresAt = now.Add(ttl)
	claims := TokenClaims{
		Typ: typ,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("jwt sign: %w", err)
	}
	return token, jti, expiresAt, nil
}

// ParseToken verifies signature, expiry (against now, 5 s leeway) and type.
func ParseToken(secret, token, typ string, now time.Time) (*TokenClaims, error) {
	claims := &TokenClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(func() time.Time { return now }),
		jwt.WithLeeway(5*time.Second),
		jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid || claims.Typ != typ || claims.Subject == "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
