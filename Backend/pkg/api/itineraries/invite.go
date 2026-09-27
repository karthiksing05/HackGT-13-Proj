package itineraries

import (
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Sentences of bringing friends along from Create (invite_user_ids).
const (
	MsgInviteFriendsOnly = "You can only add friends."
	MsgInviteTooMany     = "You can bring up to 12 friends."
	MsgInviteNoRoom      = "Not everyone fits in this group. Raise the max group size or bring fewer friends."
)

// maxInvites caps the friends one plan is saved with (after duplicates and
// the host are dropped).
const maxInvites = 12

// invitees resolves invite_user_ids for the host: duplicates and the host
// are dropped (the first mention keeps its place), at most maxInvites may
// remain, every one must be an accepted friend (400 otherwise, unknown ids
// included), and the host plus the friends must fit in max_group_size. It
// returns the friends in the order asked.
func (h *H) invitees(ctx context.Context, hostID string, req contract.CreateItineraryRequest) ([]*models.User, error) {
	seen := map[string]bool{hostID: true}
	var ids []string
	for _, id := range req.InviteUserIDs {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxInvites {
		return nil, httpx.BadRequest(MsgInviteTooMany)
	}
	pairs := make([]string, 0, len(ids))
	for _, id := range ids {
		pairs = append(pairs, models.FriendshipID(hostID, id))
	}
	friendships, err := h.d.Store.Friends().ByPairs(ctx, pairs)
	if err != nil {
		return nil, err
	}
	users, err := h.d.Store.Users().ByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	friends := make([]*models.User, 0, len(ids))
	for i, id := range ids {
		if friendships[pairs[i]] == nil || users[id] == nil {
			return nil, httpx.BadRequest(MsgInviteFriendsOnly)
		}
		friends = append(friends, users[id])
	}
	if req.MaxGroupSize != nil && 1+len(friends) > *req.MaxGroupSize {
		return nil, httpx.BadRequest(MsgInviteNoRoom)
	}
	return friends, nil
}

// bringAlong puts the friends on a plan about to be saved, after the host.
// A just_me plan with company becomes friends-visible: it is posted to the
// host's friends like any small-group plan instead of staying private.
func bringAlong(doc *models.Itinerary, friends []*models.User) {
	if len(friends) == 0 {
		return
	}
	for _, f := range friends {
		doc.MemberIDs = append(doc.MemberIDs, f.ID.Hex())
	}
	if doc.Visibility == models.VisibilityJustMe {
		doc.Visibility = models.VisibilityFriends
	}
}

// welcome finishes bringing friends into a plan that was just saved with
// them as members, the way an accepted join does: each gets a join record
// (so leaving cancels it), the plan's group thread holds everyone and opens
// with the host's line ("Jordan added Maya and Dev"), and each friend hears
// join.update and itinerary.updated (rendered for them) and, with the rest
// of the thread, thread.updated. The plan is saved already, so a failure
// here is logged rather than failing the request; the work goes on if the
// client hangs up.
func (h *H) welcome(ctx context.Context, it *models.Itinerary, host *models.User, friends []*models.User, tz *time.Location) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	ids := make([]string, 0, len(friends))
	names := make([]string, 0, len(friends))
	for _, f := range friends {
		ids = append(ids, f.ID.Hex())
		names = append(names, firstName(f))
	}
	for _, id := range ids {
		if err := h.d.Store.Joins().Record(ctx, it.ID, id); err != nil {
			log.Warn().Err(err).Str("itinerary", it.ID).Str("user", id).Msg("invite: join record not saved")
		}
	}
	threadID, err := h.openGroupThread(ctx, it, AddedLine(firstName(host), names))
	if err != nil {
		log.Warn().Err(err).Str("itinerary", it.ID).Msg("invite: group thread not ready")
	}
	result := contract.JoinResult{Status: contract.JoinJoined, ItineraryID: &it.ID}
	if threadID != "" {
		result.ThreadID = &threadID
	}
	p := h.d.Publish()
	for _, id := range ids {
		realtime.JoinUpdate(p, id, it.ID, result)
	}
	PublishUpdated(ctx, h.d, it, ids)
	if s := currentSocial(); s != nil && threadID != "" {
		s.ThreadChanged(ctx, h.d, threadID, tz)
	}
}

// openGroupThread makes the plan's group thread with every member in it,
// records it on the plan and posts line in it as the host. It returns the
// thread's id ("" when the thread could not be made).
func (h *H) openGroupThread(ctx context.Context, it *models.Itinerary, line string) (string, error) {
	st := h.d.Store
	th, _, err := st.Threads().EnsureGroup(ctx, it)
	if err != nil {
		return "", err
	}
	if err := st.Joins().SetThread(ctx, it.ID, th.ID); err != nil {
		return "", err
	}
	it.ThreadID = th.ID
	msg := &models.Message{ThreadID: th.ID, SenderID: it.HostID, Text: line}
	if _, err := st.Messages().Insert(ctx, msg); err != nil {
		return th.ID, err
	}
	if _, err := st.Threads().Touch(ctx, th, msg); err != nil {
		return th.ID, err
	}
	return th.ID, nil
}

// AddedLine is the group thread's first line: "Jordan added Maya", "Jordan
// added Maya and Dev", "Jordan added Maya, Dev and Sam".
func AddedLine(host string, friends []string) string {
	list := strings.Join(friends, ", ")
	if n := len(friends); n > 1 {
		list = strings.Join(friends[:n-1], ", ") + " and " + friends[n-1]
	}
	return host + " added " + list
}

// firstName is how the thread's line names someone: the first word of their
// name, else their handle.
func firstName(u *models.User) string {
	if fields := strings.Fields(u.Name); len(fields) > 0 {
		return fields[0]
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return "Someone"
}
