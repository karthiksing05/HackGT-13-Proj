package auth

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/util"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	resetCodeTTL     = 10 * time.Minute
	resetTokenTTL    = 15 * time.Minute
	maxResetAttempts = 5
)

// Forgot is POST /auth/password/forgot and …/resend: always 200 {} so the
// response never reveals whether the email exists. Simulated: there is no
// mail provider; the 6-digit code is written to the server log, and with
// DEV_RESET_CODES=1 also returned as {"code"} for local testing.
func (h *H) Forgot(w http.ResponseWriter, r *http.Request) {
	var req contract.EmailRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	email := util.NormalizeEmail(req.Email)
	if !h.emailAllowed(email) {
		httpx.Error(w, http.StatusTooManyRequests, httpx.TooManyRequests)
		return
	}
	resp := contract.ForgotResponse{}
	user, err := h.d.Store.Users().ByEmail(r.Context(), email)
	if err != nil && !isNotFound(err) {
		api.Fail(w, r, err)
		return
	}
	if user != nil {
		code := util.GenerateSixDigitCode()
		if err := h.d.Store.Resets().Create(r.Context(), email, util.SHA256Hex(code), resetCodeTTL); err != nil {
			api.Fail(w, r, err)
			return
		}
		// Simulated delivery: the log line is the "email".
		log.Info().Str("email", email).Str("code", code).Msg("password reset code issued (simulated delivery)")
		if h.d.Cfg.DevResetCodes {
			resp.Code = &code
		}
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// Verify is POST /auth/password/verify {email, code} → {reset_token}. A
// wrong or expired code, or a sixth attempt, is a 400 with one sentence.
func (h *H) Verify(w http.ResponseWriter, r *http.Request) {
	var req contract.VerifyResetRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	email := util.NormalizeEmail(req.Email)
	code := strings.TrimSpace(req.Code)
	pending, err := h.d.Store.Resets().Get(r.Context(), email)
	if err != nil {
		if isNotFound(err) {
			httpx.Error(w, http.StatusBadRequest, MsgBadResetCode)
			return
		}
		api.Fail(w, r, err)
		return
	}
	now := h.d.Clock()
	if pending.Attempts >= maxResetAttempts || !pending.ExpiresAt.After(now) || pending.VerifiedAt != nil {
		httpx.Error(w, http.StatusBadRequest, MsgBadResetCode)
		return
	}
	if subtle.ConstantTimeCompare([]byte(util.SHA256Hex(code)), []byte(pending.CodeHash)) != 1 {
		if err := h.d.Store.Resets().RecordAttempt(r.Context(), email); err != nil {
			api.Fail(w, r, err)
			return
		}
		httpx.Error(w, http.StatusBadRequest, MsgBadResetCode)
		return
	}
	user, err := h.d.Store.Users().ByEmail(r.Context(), email)
	if err != nil {
		if isNotFound(err) {
			httpx.Error(w, http.StatusBadRequest, MsgBadResetCode)
			return
		}
		api.Fail(w, r, err)
		return
	}
	token, jti, _, err := util.SignToken(h.d.Cfg.JWTSecret, util.TokenTypeReset, user.ID.Hex(), now, resetTokenTTL)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Resets().MarkVerified(r.Context(), email, jti); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, contract.ResetTokenResponse{ResetToken: token})
}

// Reset is POST /auth/password/reset {reset_token, new_password} → 204. The
// token is single use; every session of the user is revoked.
func (h *H) Reset(w http.ResponseWriter, r *http.Request) {
	var req contract.ResetPasswordRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	if !util.IsValidPassword(req.NewPassword) {
		httpx.Error(w, http.StatusBadRequest, MsgPassword)
		return
	}
	claims, err := util.ParseToken(h.d.Cfg.JWTSecret, req.ResetToken, util.TokenTypeReset, h.d.Clock())
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, MsgBadResetToken)
		return
	}
	user, err := h.d.Store.Users().ByID(r.Context(), claims.Subject)
	if err != nil {
		if isNotFound(err) {
			httpx.Error(w, http.StatusBadRequest, MsgBadResetToken)
			return
		}
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Resets().Consume(r.Context(), user.Email, claims.ID); err != nil {
		if isNotFound(err) {
			httpx.Error(w, http.StatusBadRequest, MsgBadResetToken)
			return
		}
		api.Fail(w, r, err)
		return
	}
	hash, err := util.HashPassword(req.NewPassword)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Users().SetPassword(r.Context(), user.ID.Hex(), hash); err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Tokens().RevokeAll(r.Context(), user.ID.Hex()); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}
