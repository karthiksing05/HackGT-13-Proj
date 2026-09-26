// Package middleware holds bearer authentication, CORS and the response
// helpers kept for compatibility (they delegate to httpx).
package middleware

import (
	"Backend/pkg/httpx"
	"Backend/pkg/util"
	"context"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const userIDKey contextKey = "user_id"

// SessionExpired is the 401 sentence; the app refreshes once, then signs out.
const SessionExpired = "Your session expired. Sign in again."

// Auth verifies access tokens signed with the server secret.
type Auth struct {
	secret string
	now    func() time.Time
}

// NewAuth builds the verifier; now is the testable clock (nil = time.Now).
func NewAuth(secret string, now func() time.Time) *Auth {
	if now == nil {
		now = time.Now
	}
	return &Auth{secret: secret, now: now}
}

// UserFromToken returns the subject of a valid access token (websocket auth, tests).
func (a *Auth) UserFromToken(token string) (string, bool) {
	claims, err := util.ParseToken(a.secret, token, util.TokenTypeAccess, a.now())
	if err != nil {
		return "", false
	}
	return claims.Subject, true
}

// bearer extracts the token from "Authorization: Bearer <token>".
func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// Bearer requires a valid access token; 401 otherwise.
func (a *Auth) Bearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearer(r)
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, SessionExpired)
			return
		}
		userID, ok := a.UserFromToken(token)
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, SessionExpired)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID)))
	})
}

// Optional attaches the user when a valid token is present and continues
// anonymously otherwise (POST /auth/logout works with an expired access token).
func (a *Auth) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := bearer(r); ok {
			if userID, ok := a.UserFromToken(token); ok {
				r = r.WithContext(WithUserID(r.Context(), userID))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// UserID is the authenticated user's id ("" when anonymous).
func UserID(r *http.Request) string {
	id, _ := r.Context().Value(userIDKey).(string)
	return id
}

// WithUserID stores a user id on a context (tests, websocket handshake).
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}
