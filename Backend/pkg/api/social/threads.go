package social

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"context"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// Sentences of the thread endpoints.
const (
	MsgDMSelf    = "You can't message yourself."
	MsgEmptyText = "Type a message first."
)

// threadLimit caps GET /threads.
const threadLimit = 200

// threadKit loads what rendering threads needs: members, the group
// threads' itineraries, photo counts and expenses, and for DMs whether the
// two are friends plus their presence.
func (h *H) threadKit(ctx context.Context, threads []*models.Thread, tz *time.Location) (*threadKit, error) {
	now := h.d.BusinessNow(ctx).In(tz)
	var members, planIDs, groupIDs, pairIDs []string
	for _, th := range threads {
		members = append(members, th.MemberIDs...)
		if th.IsGroup {
			groupIDs = append(groupIDs, th.ID)
			if th.ItineraryID != "" {
				planIDs = append(planIDs, th.ItineraryID)
			}
		} else if len(th.MemberIDs) == 2 {
			pairIDs = append(pairIDs, models.FriendshipID(th.MemberIDs[0], th.MemberIDs[1]))
		}
	}
	st := h.d.Store
	ppl, err := h.loadPeople(ctx, members)
	if err != nil {
		return nil, err
	}
	plans, err := st.Forum().Plans(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	photos, err := st.Photos().GroupPhotoCounts(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	expenses, err := st.Expenses().ListForGroups(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	pairs, err := st.Friends().ByPairs(ctx, pairIDs)
	if err != nil {
		return nil, err
	}
	var friendsInDMs []string
	for _, fs := range pairs {
		friendsInDMs = append(friendsInDMs, fs.UserIDs...)
	}
	presence, err := st.Users().Presence(ctx, friendsInDMs, now)
	if err != nil {
		return nil, err
	}
	return &threadKit{ppl: ppl, plans: plans, photos: photos, expenses: expenses, pairs: pairs, presence: presence, now: now}, nil
}

// renderThread renders one thread for the viewer.
func (h *H) renderThread(ctx context.Context, th *models.Thread, viewerID string, tz *time.Location) (contract.ChatThread, error) {
	kit, err := h.threadKit(ctx, []*models.Thread{th}, tz)
	if err != nil {
		return contract.ChatThread{}, err
	}
	return kit.render(th, viewerID), nil
}

// pushThread sends thread.updated to each recipient, rendered for them.
func (h *H) pushThread(ctx context.Context, th *models.Thread, tz *time.Location, recipients []string) {
	ctx, cancel := eventContext(ctx)
	defer cancel()
	kit, err := h.threadKit(ctx, []*models.Thread{th}, tz)
	if err != nil {
		logEventError(realtime.EventThreadUpdated, err)
		return
	}
	for _, id := range recipients {
		realtime.ThreadUpdated(h.d.Publish(), id, kit.render(th, id))
	}
}

// ListThreads is GET /threads → [ChatThread], most recent first (cap 200).
func (h *H) ListThreads(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	threads, err := h.d.Store.Threads().ListForUser(r.Context(), viewerID, threadLimit)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	kit, err := h.threadKit(r.Context(), threads, httpx.TZ(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.ChatThread, 0, len(threads))
	for _, th := range threads {
		out = append(out, kit.render(th, viewerID))
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GetThread is GET /threads/{id} → ChatThread (members only; 404 otherwise).
func (h *H) GetThread(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	th, err := h.d.Store.Threads().ForMember(r.Context(), mux.Vars(r)["id"], viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out, err := h.renderThread(r.Context(), th, viewerID, httpx.TZ(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// StartDM is POST /threads/dm {user_id} → ChatThread with that person
// (created on first use; they get thread.updated then).
func (h *H) StartDM(w http.ResponseWriter, r *http.Request) {
	var req contract.StartDMRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	viewerID := api.UserID(r)
	if req.UserID == viewerID {
		httpx.Error(w, http.StatusBadRequest, MsgDMSelf)
		return
	}
	if _, err := h.d.Store.Users().ByID(r.Context(), req.UserID); err != nil {
		api.Fail(w, r, err)
		return
	}
	th, created, err := h.d.Store.Threads().EnsureDM(r.Context(), viewerID, req.UserID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	tz := httpx.TZ(r)
	if created {
		h.pushThread(r.Context(), th, tz, []string{req.UserID})
	}
	out, err := h.renderThread(r.Context(), th, viewerID, tz)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
