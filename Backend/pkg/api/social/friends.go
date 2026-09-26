package social

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Sentences of the Friends screen.
const (
	MsgFriendSelf    = "You can't add yourself."
	MsgAlreadyFriend = "You're already friends."
)

// searchLimit caps GET /users/search.
const searchLimit = 20

// activityRank orders the Friends list: new friends, then who's free, on a
// sidequest, busy.
var activityRank = map[contract.FriendActivity]int{
	contract.ActivityNew: 0, contract.ActivityFree: 1, contract.ActivityOnSidequest: 2, contract.ActivityBusy: 3,
}

// ListFriends is GET /friends → [Friend] with live status lines.
func (h *H) ListFriends(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	ctx := r.Context()
	friendships, err := h.d.Store.Friends().Of(ctx, viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	ids := make([]string, 0, len(friendships))
	since := map[string]time.Time{}
	for _, fs := range friendships {
		id := store.FriendshipOther(fs, viewerID)
		ids = append(ids, id)
		since[id] = fs.CreatedAt
	}
	ppl, err := h.loadPeople(ctx, ids)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	now := h.d.BusinessNow(ctx).In(httpx.TZ(r))
	facts, err := h.d.Store.Users().Presence(ctx, ids, now)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.Friend, 0, len(ids))
	for _, id := range ids {
		if u, ok := ppl.byID[id]; ok {
			out = append(out, friendView(u, facts[id], since[id], now, ppl.base))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := activityRank[out[i].Activity], activityRank[out[j].Activity]; ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Person.Name) < strings.ToLower(out[j].Person.Name)
	})
	httpx.JSON(w, http.StatusOK, out)
}

// SearchUsers is GET /users/search?q= → [UserSearchResult] (≤ 20, never
// yourself) with how each person relates to you.
func (h *H) SearchUsers(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	users, err := h.d.Store.Users().Search(r.Context(), r.URL.Query().Get("q"), viewerID, searchLimit)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out, err := h.searchResults(r.Context(), viewerID, users)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// searchResults renders people with their relation to the viewer.
func (h *H) searchResults(ctx context.Context, viewerID string, users []*models.User) ([]contract.UserSearchResult, error) {
	ids := make([]string, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID.Hex())
	}
	relations, err := h.d.Store.Friends().Relations(ctx, viewerID, ids)
	if err != nil {
		return nil, err
	}
	out := make([]contract.UserSearchResult, 0, len(users))
	for _, u := range users {
		rel := relations[u.ID.Hex()]
		row := contract.UserSearchResult{Person: view.PersonRef(u, h.d.Cfg.PublicBaseURL), Relation: contract.FriendRelation(rel.Kind)}
		if rel.RequestID != "" {
			id := rel.RequestID
			row.RequestID = &id
		}
		out = append(out, row)
	}
	return out, nil
}

// ListRequests is GET /friends/requests → incoming requests, then the ones
// the viewer sent (outgoing: true).
func (h *H) ListRequests(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	incoming, outgoing, err := h.d.Store.Friends().Pending(r.Context(), viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	var ids []string
	for _, req := range append(append([]*models.FriendRequest{}, incoming...), outgoing...) {
		ids = append(ids, req.FromID, req.ToID)
	}
	ppl, err := h.loadPeople(r.Context(), ids)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	now := h.d.BusinessNow(r.Context())
	out := make([]contract.FriendRequest, 0, len(incoming)+len(outgoing))
	for _, req := range append(incoming, outgoing...) {
		if view, ok := requestView(req, viewerID, ppl, now); ok {
			out = append(out, view)
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// SendRequest is POST /friends/requests {user_id} → 201 FriendRequest
// (outgoing). A request already sent comes back as is (200); when they had
// already asked the viewer, this accepts theirs.
func (h *H) SendRequest(w http.ResponseWriter, r *http.Request) {
	var req contract.StartDMRequest
	if !httpx.Decode(w, r, &req) {
		return
	}
	viewerID := api.UserID(r)
	ctx := r.Context()
	if req.UserID == viewerID {
		httpx.Error(w, http.StatusBadRequest, MsgFriendSelf)
		return
	}
	if _, err := h.d.Store.Users().ByID(ctx, req.UserID); err != nil {
		api.Fail(w, r, err)
		return
	}
	switch _, err := h.d.Store.Friends().Get(ctx, viewerID, req.UserID); {
	case err == nil:
		httpx.Error(w, http.StatusConflict, MsgAlreadyFriend)
		return
	case !errors.Is(err, store.ErrNotFound):
		api.Fail(w, r, err)
		return
	}
	tz := httpx.TZ(r)
	ppl, err := h.loadPeople(ctx, []string{viewerID, req.UserID})
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	now := h.d.BusinessNow(ctx)
	// They asked first: sending back means yes.
	theirs, err := h.d.Store.Friends().PendingFrom(ctx, req.UserID, viewerID)
	switch {
	case err == nil:
		if _, err := h.befriend(ctx, viewerID, req.UserID, tz); err != nil {
			api.Fail(w, r, err)
			return
		}
		out, _ := requestView(theirs, viewerID, ppl, now)
		out.Note = "You're friends now"
		httpx.JSON(w, http.StatusOK, out)
		return
	case !errors.Is(err, store.ErrNotFound):
		api.Fail(w, r, err)
		return
	}
	sent, created, err := h.d.Store.Friends().CreateRequest(ctx, viewerID, req.UserID, "")
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out, _ := requestView(sent, viewerID, ppl, now)
	if !created {
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	if incoming, ok := requestView(sent, req.UserID, ppl, now); ok {
		realtime.FriendRequest(h.d.Publish(), req.UserID, incoming)
	}
	realtime.FriendRequest(h.d.Publish(), viewerID, out) // the sender's other devices
	httpx.JSON(w, http.StatusCreated, out)
}

// CancelRequest is DELETE /friends/requests/{id}: the sender withdraws their
// own pending request → 204.
func (h *H) CancelRequest(w http.ResponseWriter, r *http.Request) {
	if _, err := h.d.Store.Friends().Cancel(r.Context(), mux.Vars(r)["id"], api.UserID(r)); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// AcceptRequest is POST /friends/requests/{id}/accept (recipient only) →
// 204: the two become friends with a DM thread.
func (h *H) AcceptRequest(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	req, err := h.d.Store.Friends().Answer(r.Context(), mux.Vars(r)["id"], viewerID, models.RequestAccepted)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if _, err := h.befriend(r.Context(), viewerID, req.FromID, httpx.TZ(r)); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// DeclineRequest is POST /friends/requests/{id}/decline (recipient only) → 204.
func (h *H) DeclineRequest(w http.ResponseWriter, r *http.Request) {
	if _, err := h.d.Store.Friends().Answer(r.Context(), mux.Vars(r)["id"], api.UserID(r), models.RequestDeclined); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// RemoveFriend is DELETE /friends/{user_id} → 204. The DM stays; both sides
// get it back (thread.updated) since its subtitle no longer shows a status.
func (h *H) RemoveFriend(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	otherID := mux.Vars(r)["user_id"]
	ctx := r.Context()
	if _, err := h.d.Store.Users().ByID(ctx, otherID); err != nil {
		api.Fail(w, r, err)
		return
	}
	removed, err := h.d.Store.Friends().Unfriend(ctx, viewerID, otherID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if removed {
		switch th, err := h.d.Store.Threads().DM(ctx, viewerID, otherID); {
		case err == nil:
			h.pushThread(ctx, th, httpx.TZ(r), th.MemberIDs)
		case !errors.Is(err, store.ErrNotFound):
			logEventError(realtime.EventThreadUpdated, err)
		}
	}
	httpx.NoContent(w)
}

// befriend makes a and b friends with a DM thread; when that is new, both
// hear each other's status line (friend.status) and get the DM
// (thread.updated). created is false when they already were friends.
func (h *H) befriend(ctx context.Context, a, b string, tz *time.Location) (bool, error) {
	_, created, err := h.d.Store.Friends().Befriend(ctx, a, b)
	if err != nil {
		return false, err
	}
	th, _, err := h.d.Store.Threads().EnsureDM(ctx, a, b)
	if err != nil {
		return false, err
	}
	if created {
		ectx, cancel := eventContext(ctx)
		logEventError(realtime.EventFriendStatus, announcePresence(ectx, h.d, a, tz, b))
		logEventError(realtime.EventFriendStatus, announcePresence(ectx, h.d, b, tz, a))
		cancel()
		h.pushThread(ctx, th, tz, th.MemberIDs)
	}
	return created, nil
}
