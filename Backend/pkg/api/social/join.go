package social

import (
	"Backend/pkg/api"
	"Backend/pkg/api/itineraries"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

// Sentences of joining and "Plan together".
const (
	MsgJoinFreePost = "Use Plan together for this post."
	MsgJoinOwnPlan  = "You host this sidequest."
	MsgPlanTogether = "Saw your post. Want to plan something together?"
	MsgOwnPost      = "That's your own post."
)

// planTogetherTag prefixes the client id of the "Plan together" message, so
// the messages index keeps it to one per post.
const planTogetherTag = "plan-together:"

// visiblePlan is a shared plan the viewer may see: a friend's, an open
// one, or one they are in; anything else is ErrNotFound.
func (h *H) visiblePlan(ctx context.Context, id, viewerID string) (*models.Itinerary, error) {
	it, err := h.d.Store.Forum().SharedPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	if it.Visibility == models.VisibilityOpen || it.IsMember(viewerID) {
		return it, nil
	}
	if _, err := h.d.Store.Friends().Get(ctx, it.HostID, viewerID); err != nil {
		return nil, err
	}
	return it, nil
}

// Join is POST /forum/posts/{id}/join-requests → JoinResult. Joins are
// accepted on the spot: the viewer becomes a member, the group thread
// exists and includes them → {joined, itinerary_id, thread_id}. A locked or
// started plan answers closed, a full one full.
func (h *H) Join(w http.ResponseWriter, r *http.Request) {
	viewer, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	viewerID := viewer.ID.Hex()
	ctx := r.Context()
	id := mux.Vars(r)["id"]
	now := h.d.Clock()
	switch _, err := h.d.Store.Forum().FreePost(ctx, id, now); {
	case err == nil:
		httpx.Error(w, http.StatusBadRequest, MsgJoinFreePost)
		return
	case !errors.Is(err, store.ErrNotFound):
		api.Fail(w, r, err)
		return
	}
	it, err := h.visiblePlan(ctx, id, viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if it.HostID == viewerID {
		httpx.Error(w, http.StatusBadRequest, MsgJoinOwnPlan)
		return
	}
	added := false
	if !it.IsMember(viewerID) {
		it, added, err = h.d.Store.Joins().AddMember(ctx, id, viewerID, now)
		switch {
		case errors.Is(err, store.ErrPlanClosed):
			httpx.JSON(w, http.StatusOK, contract.JoinResult{Status: contract.JoinClosed})
			return
		case errors.Is(err, store.ErrPlanFull):
			httpx.JSON(w, http.StatusOK, contract.JoinResult{Status: contract.JoinFull})
			return
		case err != nil:
			api.Fail(w, r, err)
			return
		}
	}
	th, err := h.groupThread(ctx, it)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	itineraryID, threadID := it.ID, th.ID
	result := contract.JoinResult{Status: contract.JoinJoined, ItineraryID: &itineraryID, ThreadID: &threadID}
	if added {
		if err := h.d.Store.Joins().Record(ctx, it.ID, viewerID); err != nil {
			api.Fail(w, r, err)
			return
		}
		h.pushJoin(ctx, it, th, viewer, result, httpx.TZ(r))
	}
	httpx.JSON(w, http.StatusOK, result)
}

// groupThread makes sure the itinerary's group chat exists with every
// member in it and is recorded on the itinerary.
func (h *H) groupThread(ctx context.Context, it *models.Itinerary) (*models.Thread, error) {
	th, _, err := h.d.Store.Threads().EnsureGroup(ctx, it)
	if err != nil {
		return nil, err
	}
	if it.ThreadID == "" {
		if err := h.d.Store.Joins().SetThread(ctx, it.ID, th.ID); err != nil {
			return nil, err
		}
		it.ThreadID = th.ID
	}
	return th, nil
}

// pushJoin tells the joiner (join.update), the host (join.request) and every
// member (itinerary.updated and thread.updated, each rendered for them),
// then asks every feed to refresh (forum.update).
func (h *H) pushJoin(ctx context.Context, it *models.Itinerary, th *models.Thread, joiner *models.User, result contract.JoinResult, tz *time.Location) {
	p := h.d.Publish()
	realtime.JoinUpdate(p, joiner.ID.Hex(), it.ID, result)
	realtime.JoinRequest(p, it.HostID, it.ID, view.PersonRef(joiner, h.d.Cfg.PublicBaseURL))
	h.pushItinerary(ctx, it, it.MemberIDs)
	h.pushThread(ctx, th, tz, th.MemberIDs)
	realtime.ForumUpdate(p)
}

// CancelJoin is DELETE /forum/posts/{id}/join-requests → 204. A member
// leaves exactly as with POST /itineraries/{id}/leave (itineraries.Leave):
// out of the plan and its group chat with their join cancelled;
// itinerary.removed to them, itinerary.updated and thread.updated to the
// rest, forum.update; the host gets 400. Anyone else only has their join
// record, if any, marked cancelled.
func (h *H) CancelJoin(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	ctx := r.Context()
	it, err := h.d.Store.Forum().ActivePlan(ctx, mux.Vars(r)["id"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if !it.IsMember(viewerID) {
		if err := h.d.Store.Joins().Cancel(ctx, it.ID, viewerID); err != nil {
			api.Fail(w, r, err)
			return
		}
		httpx.NoContent(w)
		return
	}
	// ErrNotFound: they left in the meantime, which is what they asked for.
	if err := itineraries.Leave(ctx, h.d, it, viewerID, httpx.TZ(r)); err != nil && !errors.Is(err, store.ErrNotFound) {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// PlanTogether is POST /forum/posts/{id}/plan-together → ChatThread: the DM
// with the post's author, where the viewer says "Saw your post. Want to
// plan something together?" once per post (again → the same DM, no second
// message).
func (h *H) PlanTogether(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	ctx := r.Context()
	id := mux.Vars(r)["id"]
	authorID, err := h.postAuthor(ctx, id, viewerID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if authorID == viewerID {
		httpx.Error(w, http.StatusBadRequest, MsgOwnPost)
		return
	}
	if _, err := h.d.Store.Users().ByID(ctx, authorID); err != nil {
		api.Fail(w, r, err)
		return
	}
	tz := httpx.TZ(r)
	th, created, err := h.d.Store.Threads().EnsureDM(ctx, viewerID, authorID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	msg := &models.Message{ThreadID: th.ID, SenderID: viewerID, Text: MsgPlanTogether, ClientID: planTogetherTag + id}
	sent, err := h.d.Store.Messages().Insert(ctx, msg)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	switch {
	case sent:
		if th, err = h.d.Store.Threads().Touch(ctx, th, msg); err != nil {
			api.Fail(w, r, err)
			return
		}
		h.pushMessage(ctx, th, msg, tz)
	case created:
		h.pushThread(ctx, th, tz, []string{authorID})
	}
	if _, err := h.d.Store.PlanTogether().Mark(ctx, id, viewerID, th.ID); err != nil {
		api.Fail(w, r, err)
		return
	}
	out, err := h.renderThread(ctx, th, viewerID, tz)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// postAuthor resolves a post id the viewer may see to its author: a live
// free-now post (friends-only ones for friends) or a shared plan's host.
func (h *H) postAuthor(ctx context.Context, id, viewerID string) (string, error) {
	post, err := h.d.Store.Forum().FreePost(ctx, id, h.d.Clock())
	switch {
	case err == nil:
		if post.Visibility == models.PostVisibilityFriends && post.AuthorID != viewerID {
			if _, err := h.d.Store.Friends().Get(ctx, post.AuthorID, viewerID); err != nil {
				return "", err
			}
		}
		return post.AuthorID, nil
	case !errors.Is(err, store.ErrNotFound):
		return "", err
	}
	it, err := h.visiblePlan(ctx, id, viewerID)
	if err != nil {
		return "", err
	}
	return it.HostID, nil
}
