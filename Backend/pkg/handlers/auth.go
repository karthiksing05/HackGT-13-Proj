package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

type SignupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type AuthResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	User         *models.User `json:"user"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type VerifyPasswordRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type ResetPasswordRequest struct {
	ResetToken  string `json:"reset_token"`
	NewPassword string `json:"new_password"`
}

var (
	tokenStoreMu sync.RWMutex
	userByToken  = make(map[string]string) // refreshToken -> userID
)

func Signup(w http.ResponseWriter, r *http.Request) {
	var req SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Name = strings.TrimSpace(req.Name)

	if !util.IsValidEmail(req.Email) {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid email format")
		return
	}
	if !util.IsValidPassword(req.Password) {
		middleware.WriteError(w, http.StatusBadRequest, "Password must be at least 6 characters")
		return
	}
	if len(req.Name) < 2 {
		middleware.WriteError(w, http.StatusBadRequest, "Name must be at least 2 characters")
		return
	}

	hash, err := util.HashPassword(req.Password)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	u, err := store.GlobalStore.CreateUser(req.Email, hash, req.Name)
	if err != nil {
		middleware.WriteError(w, http.StatusConflict, err.Error())
		return
	}

	usernameStr := ""
	if u.Username != nil {
		usernameStr = *u.Username
	}

	accessToken, err := util.GenerateAccessToken(u.ID.Hex(), u.Email, usernameStr)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to generate access token")
		return
	}

	refreshToken, err := util.GenerateRefreshToken()
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to generate refresh token")
		return
	}

	tokenStoreMu.Lock()
	userByToken[refreshToken] = u.ID.Hex()
	tokenStoreMu.Unlock()

	middleware.WriteJSON(w, http.StatusCreated, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         u,
	})
}

func Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	u, err := store.GlobalStore.GetUserByEmail(req.Email)
	if err != nil {
		middleware.WriteError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	valid, err := util.VerifyPassword(req.Password, u.PasswordHash)
	if err != nil || !valid {
		middleware.WriteError(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	usernameStr := ""
	if u.Username != nil {
		usernameStr = *u.Username
	}

	accessToken, err := util.GenerateAccessToken(u.ID.Hex(), u.Email, usernameStr)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to generate access token")
		return
	}

	refreshToken, err := util.GenerateRefreshToken()
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to generate refresh token")
		return
	}

	tokenStoreMu.Lock()
	userByToken[refreshToken] = u.ID.Hex()
	tokenStoreMu.Unlock()

	middleware.WriteJSON(w, http.StatusOK, AuthResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         u,
	})
}

func Refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		middleware.WriteError(w, http.StatusBadRequest, "Missing or invalid refresh_token")
		return
	}

	tokenStoreMu.RLock()
	userID, ok := userByToken[req.RefreshToken]
	tokenStoreMu.RUnlock()

	if !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "Invalid refresh token")
		return
	}

	u, err := store.GlobalStore.GetUserByID(userID)
	if err != nil {
		middleware.WriteError(w, http.StatusUnauthorized, "User not found")
		return
	}

	usernameStr := ""
	if u.Username != nil {
		usernameStr = *u.Username
	}

	newAccessToken, err := util.GenerateAccessToken(u.ID.Hex(), u.Email, usernameStr)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to generate access token")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"access_token": newAccessToken,
	})
}

func Logout(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	if uid != "" {
		tokenStoreMu.Lock()
		for token, id := range userByToken {
			if id == uid {
				delete(userByToken, token)
			}
		}
		tokenStoreMu.Unlock()
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Successfully logged out",
	})
}

func ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Email != "" {
		store.GlobalStore.CreatePasswordReset(req.Email)
	}

	// Always return 200, don't reveal whether the email exists
	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "If the account exists, a 6-digit verification code has been sent",
	})
}

func VerifyPasswordCode(w http.ResponseWriter, r *http.Request) {
	var req VerifyPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	resetToken, err := store.GlobalStore.VerifyPasswordReset(req.Email, req.Code)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"reset_token": resetToken,
	})
}

func ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !util.IsValidPassword(req.NewPassword) {
		middleware.WriteError(w, http.StatusBadRequest, "Password must be at least 6 characters")
		return
	}

	claims, err := util.ValidateToken(req.ResetToken)
	if err != nil || claims.Email == "" {
		middleware.WriteError(w, http.StatusUnauthorized, "Invalid or expired reset token")
		return
	}

	hash, err := util.HashPassword(req.NewPassword)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to hash password")
		return
	}

	if err := store.GlobalStore.ResetPassword(claims.Email, hash); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Password successfully reset. Other sessions signed out.",
	})
}

func ResendPasswordCode(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Email != "" {
		store.GlobalStore.CreatePasswordReset(req.Email)
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Verification code resent if account exists",
	})
}
