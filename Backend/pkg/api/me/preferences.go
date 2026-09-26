package me

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"
)

// profileRefreshTimeout bounds the synchronous taste-vector refresh after a
// preferences save (embeddings.md §2).
const profileRefreshTimeout = 15 * time.Second

// maxAnswerRunes caps each open answer (Setup step 5, typed or spoken);
// MsgAnswerTooLong says so.
const (
	maxAnswerRunes   = 2000
	MsgAnswerTooLong = "Keep each answer under 2,000 characters."
)

// GetPreferences is GET /me/preferences → Preferences (the defaults until the
// first save).
func (h *H) GetPreferences(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, preferencesView(user.Prefs))
}

// PutPreferences is PUT /me/preferences (Preferences) → 200 Preferences. It
// validates enums, rating keys and 1–5 values, keeps the three known answers,
// marks setup complete, then refreshes the taste vectors synchronously. A
// failing ML service never fails the save: the old vectors stay and the
// profile hash is cleared so the next ranking rebuilds them.
func (h *H) PutPreferences(w http.ResponseWriter, r *http.Request) {
	var req contract.Preferences
	if !httpx.Decode(w, r, &req) {
		return
	}
	if req.Pace == "chill" { // the Setup label for relaxed
		req.Pace = contract.PaceRelaxed
	}
	if err := req.Validate(); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	answers, msg := cleanAnswers(req.Answers)
	if msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	userID := user.ID.Hex()
	user, err = h.d.Store.Users().SavePrefs(r.Context(), userID, prefsDoc(req, answers))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	// The save stands even if the caller hangs up; finish the refresh.
	ctx := context.WithoutCancel(r.Context())
	if err := h.d.RefreshProfile(ctx, userID, profileRefreshTimeout); err != nil {
		log.Warn().Err(err).Str("user", userID).Msg("profile refresh after preferences failed; old vectors kept")
		if err := h.d.Store.Users().Unset(ctx, userID, "profileTextHash"); err != nil {
			log.Warn().Err(err).Str("user", userID).Msg("clear profile hash")
		}
	}
	httpx.JSON(w, http.StatusOK, preferencesView(user.Prefs))
}

// preferencesView renders stored preferences; never-saved ones read as the
// defaults, and an empty stored enum as its default.
func preferencesView(p models.UserPrefs) contract.Preferences {
	def := contract.DefaultPreferences()
	if !p.IsSet() {
		return def
	}
	out := contract.Preferences{
		Ratings:                   contract.Ratings{},
		Company:                   orDefault(contract.Company(p.Company), def.Company),
		Pace:                      orDefault(contract.Pace(p.Pace), def.Pace),
		Spend:                     orDefault(contract.SpendTier(p.Spend), def.Spend),
		Flexibility:               orDefault(contract.Flexibility(p.Flexibility), def.Flexibility),
		SplitStyle:                orDefault(contract.SplitStyle(p.SplitStyle), def.SplitStyle),
		PreferFree:                p.PreferFree,
		Answers:                   contract.Answers{},
		InstantCheckout:           p.InstantCheckout,
		InstantCheckoutLimitCents: p.InstantCheckoutLimitCents,
	}
	for k, v := range p.Ratings {
		out.Ratings[k] = v
	}
	for k, v := range p.Answers {
		out.Answers[k] = v
	}
	return out
}

func orDefault[T ~string](v, def T) T {
	if v == "" {
		return def
	}
	return v
}

// cleanAnswers keeps the known answer keys with trimmed, non-empty text; it
// returns a sentence when an answer is too long.
func cleanAnswers(in contract.Answers) (map[string]string, string) {
	out := map[string]string{}
	for _, key := range contract.AnswerKeys {
		text := strings.TrimSpace(in[key])
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > maxAnswerRunes {
			return nil, MsgAnswerTooLong
		}
		out[key] = text
	}
	return out, ""
}

// prefsDoc is the stored shape of validated preferences.
func prefsDoc(p contract.Preferences, answers map[string]string) models.UserPrefs {
	ratings := make(map[string]int, len(p.Ratings))
	for k, v := range p.Ratings {
		ratings[k] = v
	}
	return models.UserPrefs{
		Ratings:                   ratings,
		Company:                   string(p.Company),
		Pace:                      string(p.Pace),
		Spend:                     string(p.Spend),
		Flexibility:               string(p.Flexibility),
		SplitStyle:                string(p.SplitStyle),
		PreferFree:                p.PreferFree,
		Answers:                   answers,
		InstantCheckout:           p.InstantCheckout,
		InstantCheckoutLimitCents: p.InstantCheckoutLimitCents,
	}
}
