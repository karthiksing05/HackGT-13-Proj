package middleware

import (
	"Backend/pkg/util"
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	UserClaimsKey contextKey = "user_claims"
	UserIDKey     contextKey = "user_id"
)

// BearerAuth validates Bearer JWT token on protected endpoints
func BearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			WriteError(w, http.StatusUnauthorized, "Missing Authorization header")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			WriteError(w, http.StatusUnauthorized, "Invalid Authorization header format. Expected 'Bearer <token>'")
			return
		}

		tokenStr := strings.TrimSpace(parts[1])
		claims, err := util.ValidateToken(tokenStr)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), UserClaimsKey, claims)
		ctx = context.WithValue(ctx, UserUserIDKey(UserIDKey), claims.UserID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func UserUserIDKey(k contextKey) contextKey {
	return k
}

// GetUserClaims retrieves claims from the context
func GetUserClaims(r *http.Request) *util.Claims {
	claims, ok := r.Context().Value(UserClaimsKey).(*util.Claims)
	if !ok {
		return nil
	}
	return claims
}

// GetUserID retrieves the authenticated user's ID from the context
func GetUserID(r *http.Request) string {
	if claims := GetUserClaims(r); claims != nil {
		return claims.UserID
	}
	if uid, ok := r.Context().Value(UserIDKey).(string); ok {
		return uid
	}
	return ""
}
