package social

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/store"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// Sentences of invite links.
const (
	MsgInviteBroken = "That invite link didn't work. Ask for a new one."
	MsgInviteOwn    = "That's your own invite link. Send it to a friend."
)

// InviteBaseURL is where invite links point; the app also opens
// sidequestz://invite/<code>.
const InviteBaseURL = "https://sidequests.app/invite/"

// inviteTTL is how long an invite link works.
const inviteTTL = 30 * 24 * time.Hour

// CreateInvite is POST /invites → 201 {url} (the live link is reused while
// it lasts: 200).
func (h *H) CreateInvite(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	invites := h.d.Store.Invites()
	switch inv, err := invites.Live(r.Context(), viewerID, h.d.Clock()); {
	case err == nil:
		httpx.JSON(w, http.StatusOK, contract.URLResponse{URL: InviteBaseURL + inv.Code})
		return
	case !errors.Is(err, store.ErrNotFound):
		api.Fail(w, r, err)
		return
	}
	inv, err := invites.Create(r.Context(), viewerID, inviteTTL)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, contract.URLResponse{URL: InviteBaseURL + inv.Code})
}

// AcceptInvite is POST /invites/{code}/accept → Friend (the inviter). The
// two become friends with a DM thread; an unknown, expired or own code is
// a 400.
func (h *H) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	ctx := r.Context()
	now := h.d.Clock()
	inv, err := h.d.Store.Invites().Get(ctx, mux.Vars(r)["code"])
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusBadRequest, MsgInviteBroken)
		return
	case err != nil:
		api.Fail(w, r, err)
		return
	case !inv.ExpiresAt.After(now):
		httpx.Error(w, http.StatusBadRequest, MsgInviteBroken)
		return
	case inv.UserID == viewerID:
		httpx.Error(w, http.StatusBadRequest, MsgInviteOwn)
		return
	}
	inviter, err := h.d.Store.Users().ByID(ctx, inv.UserID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Error(w, http.StatusBadRequest, MsgInviteBroken)
		return
	}
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	tz := httpx.TZ(r)
	created, err := h.befriend(ctx, viewerID, inv.UserID, tz)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if created {
		if err := h.d.Store.Invites().Use(ctx, inv.Code); err != nil {
			api.Fail(w, r, err)
			return
		}
	}
	friendship, err := h.d.Store.Friends().Get(ctx, viewerID, inv.UserID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	local := now.In(tz)
	facts, err := h.d.Store.Users().Presence(ctx, []string{inv.UserID}, local)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, friendView(inviter, facts[inv.UserID], friendship.CreatedAt, local, h.d.Cfg.PublicBaseURL))
}
