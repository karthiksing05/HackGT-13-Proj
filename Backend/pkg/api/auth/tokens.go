package auth

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"errors"
	"net/http"
)

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

// issue signs an access JWT and opens a new refresh-token family.
func (h *H) issue(r *http.Request, user *models.User) (contract.Tokens, error) {
	now := h.d.Clock()
	access, _, expiresAt, err := util.SignToken(h.d.Cfg.JWTSecret, util.TokenTypeAccess, user.ID.Hex(), now, h.d.Cfg.AccessTokenTTL)
	if err != nil {
		return contract.Tokens{}, err
	}
	refresh, err := h.d.Store.Tokens().Issue(r.Context(), user.ID.Hex(), h.d.Cfg.RefreshTokenTTL)
	if err != nil {
		return contract.Tokens{}, err
	}
	return contract.Tokens{AccessToken: access, RefreshToken: refresh, ExpiresAt: contract.NewTime(expiresAt)}, nil
}

// Refresh is POST /auth/refresh: rotate the refresh token, mint a new
// access token. A replayed token revokes its family; every failure is a 401.
func (h *H) Refresh(w http.ResponseWriter, r *http.Request) {
	var req contract.RefreshRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		httpx.Error(w, http.StatusUnauthorized, MsgBadRefresh)
		return
	}
	newRefresh, userID, err := h.d.Store.Tokens().Rotate(r.Context(), req.RefreshToken, h.d.Cfg.RefreshTokenTTL)
	if err != nil {
		if isNotFound(err) {
			httpx.Error(w, http.StatusUnauthorized, MsgBadRefresh)
			return
		}
		api.Fail(w, r, err)
		return
	}
	now := h.d.Clock()
	access, _, expiresAt, err := util.SignToken(h.d.Cfg.JWTSecret, util.TokenTypeAccess, userID, now, h.d.Cfg.AccessTokenTTL)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, contract.RefreshResponse{AccessToken: access, RefreshToken: newRefresh, ExpiresAt: contract.NewTime(expiresAt)})
}

// Logout is POST /auth/logout {refresh_token?} → 204. Public: it must work
// with an expired access token, and an unknown token is not an error.
func (h *H) Logout(w http.ResponseWriter, r *http.Request) {
	var req contract.LogoutRequest
	if !httpx.DecodeOptional(w, r, &req) {
		return
	}
	if req.RefreshToken != nil && *req.RefreshToken != "" {
		if err := h.d.Store.Tokens().Revoke(r.Context(), *req.RefreshToken); err != nil {
			api.Fail(w, r, err)
			return
		}
	}
	httpx.NoContent(w)
}
