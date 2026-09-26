// Package auth is sign-up, login, token refresh and password reset
// (backend-contract §2.4, §4 A).
package auth

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/util"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// User-facing sentences.
const (
	MsgName          = "Enter your name (at least 2 characters)."
	MsgEmail         = "Enter a valid email address."
	MsgPassword      = "Use at least 8 characters with a number."
	MsgUsername      = "Usernames are 3–30 characters: letters, numbers, dots or underscores."
	MsgUnder13       = "You need to be 13 or older to use SideQuests."
	MsgBadLogin      = "That email and password don't match."
	MsgBadRefresh    = "Your session expired. Sign in again."
	MsgBadResetCode  = "That code didn't work. Check the email or resend a new one."
	MsgBadResetToken = "That reset link expired. Start over from Forgot password."
)

// Defaults for a new account.
const (
	defaultAvatar  = string(contract.AvatarInk)
	defaultStatus  = string(contract.StatusOpen)
	defaultCatalog = "activities"
	defaultCity    = "atlanta"
)

// schools maps email domains to the school shown after the handle.
var schools = map[string]string{
	"gatech.edu": "Georgia Tech",
	"emory.edu":  "Emory",
	"gsu.edu":    "Georgia State",
}

// dummyHash is verified against when the email is unknown so a wrong email
// costs the same time as a wrong password.
var dummyHash = func() string {
	h, err := util.HashPassword(util.RandomToken(24))
	if err != nil {
		panic("auth: " + err.Error())
	}
	return h
}()

// H holds the handlers; limiters are nil when rate limits are disabled.
type H struct {
	d            *api.Deps
	emailLimiter *httpx.Limiter
}

// Register mounts every /auth route. The whole prefix is limited to
// 10 req/min per IP (burst 20); login, forgot and resend add 5/min per email.
func Register(r *mux.Router, d *api.Deps) {
	h := &H{d: d}
	s := r.PathPrefix("/auth").Subrouter()
	if !d.Cfg.DisableRateLimits {
		ip := httpx.NewLimiter(10, 20)
		s.Use(ip.Middleware(func(r *http.Request) string { return httpx.ClientIP(r, d.Cfg.TrustProxy) }))
		h.emailLimiter = httpx.NewLimiter(5, 5)
	}
	s.HandleFunc("/signup", h.Signup).Methods("POST")
	s.HandleFunc("/login", h.Login).Methods("POST")
	s.HandleFunc("/refresh", h.Refresh).Methods("POST")
	s.Handle("/logout", d.Optional(h.Logout)).Methods("POST")
	s.HandleFunc("/password/forgot", h.Forgot).Methods("POST")
	s.HandleFunc("/password/resend", h.Forgot).Methods("POST")
	s.HandleFunc("/password/verify", h.Verify).Methods("POST")
	s.HandleFunc("/password/reset", h.Reset).Methods("POST")
}

// emailAllowed applies the per-email limit; true when limits are off.
func (h *H) emailAllowed(email string) bool {
	return h.emailLimiter == nil || h.emailLimiter.Allow(email)
}

// School derives the school from an email domain ("" when unknown).
func School(email string) *string {
	_, domain, ok := strings.Cut(strings.ToLower(email), "@")
	if !ok {
		return nil
	}
	for suffix, name := range schools {
		if domain == suffix || strings.HasSuffix(domain, "."+suffix) {
			return &name
		}
	}
	return nil
}

// Signup is POST /auth/signup → 201 AuthResponse.
func (h *H) Signup(w http.ResponseWriter, r *http.Request) {
	var req contract.SignupRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	email := util.NormalizeEmail(req.Email)
	switch {
	case !util.IsValidName(name):
		httpx.Error(w, http.StatusBadRequest, MsgName)
		return
	case !util.IsValidEmail(email):
		httpx.Error(w, http.StatusBadRequest, MsgEmail)
		return
	case !util.IsValidPassword(req.Password):
		httpx.Error(w, http.StatusBadRequest, MsgPassword)
		return
	}
	now := h.d.Clock()
	var username string
	if req.Username != nil && strings.Trim(strings.TrimSpace(*req.Username), "@ ") != "" {
		handle, ok := util.NormalizeUsername(*req.Username)
		if !ok {
			httpx.Error(w, http.StatusBadRequest, MsgUsername)
			return
		}
		username = handle
	} else {
		username = util.GenerateUsername(name)
	}
	if req.DateOfBirth != nil && view.Age(req.DateOfBirth.Time, now) < 13 {
		httpx.Error(w, http.StatusBadRequest, MsgUnder13)
		return
	}
	hash, err := util.HashPassword(req.Password)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	user := &models.User{
		Email:        email,
		PasswordHash: hash,
		Name:         name,
		Username:     username,
		AvatarColor:  defaultAvatar,
		Status:       defaultStatus,
		BirthDate:    req.DateOfBirth.StdPtr(),
		School:       School(email),
		Catalog:      defaultCatalog,
		City:         defaultCity,
		Prefs:        models.UserPrefs{Ratings: map[string]int{}, Answers: map[string]string{}},
		Taste:        models.UserTaste{Tags: map[string]float64{}},
	}
	if err := h.d.Store.Users().Create(r.Context(), user); err != nil {
		api.Fail(w, r, err)
		return
	}
	tokens, err := h.issue(r, user)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	// Seam (backend-A + ml): kick off the async profile refresh here once
	// ml.UserProfile exists; a fresh account has no preferences yet, so the
	// first meaningful refresh happens on PUT /me/preferences.
	httpx.JSON(w, http.StatusCreated, contract.AuthResponse{User: view.User(user, h.d.Cfg.PublicBaseURL, now), Tokens: tokens})
}

// Login is POST /auth/login → AuthResponse; 401 with one sentence for every failure.
func (h *H) Login(w http.ResponseWriter, r *http.Request) {
	var req contract.LoginRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	email := util.NormalizeEmail(req.Email)
	if !h.emailAllowed(email) {
		httpx.Error(w, http.StatusTooManyRequests, httpx.TooManyRequests)
		return
	}
	user, err := h.d.Store.Users().ByEmail(r.Context(), email)
	if err != nil && !errors.Is(err, errNotFoundAlias) && !isNotFound(err) {
		api.Fail(w, r, err)
		return
	}
	hash := dummyHash
	if user != nil {
		hash = user.PasswordHash
	}
	ok, verr := util.VerifyPassword(req.Password, hash)
	if user == nil || verr != nil || !ok {
		httpx.Error(w, http.StatusUnauthorized, MsgBadLogin)
		return
	}
	tokens, err := h.issue(r, user)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Users().Touch(r.Context(), user.ID.Hex()); err != nil {
		log.Warn().Err(err).Msg("touch lastActiveAt")
	}
	httpx.JSON(w, http.StatusOK, contract.AuthResponse{User: view.User(user, h.d.Cfg.PublicBaseURL, h.d.Clock()), Tokens: tokens})
}
