package social

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"context"
	"time"
)

// justAddedFor is how long a new friend reads "Just added".
const justAddedFor = 24 * time.Hour

// statusLine is how a friend's row reads (now is in the viewer's zone):
// busy → "Busy"; a live free post → "Free until 6:30 PM"; a sidequest
// running now → "On a sidequest · <title>" (no title for a just-me plan);
// a friendship under a day old → "Just added"; else "Open to plans". Busy
// comes first because a busy person is hidden from friends' free lists.
func statusLine(u *models.User, facts store.PresenceFacts, friendsSince, now time.Time) (string, contract.FriendActivity) {
	switch {
	case u.Status == string(contract.StatusBusy):
		return "Busy", contract.ActivityBusy
	case facts.FreeUntil != nil:
		return "Free until " + httpx.ClockShort(facts.FreeUntil.In(now.Location())), contract.ActivityFree
	case facts.Sidequest != nil:
		if facts.Sidequest.Visibility == models.VisibilityJustMe {
			return "On a sidequest", contract.ActivityOnSidequest
		}
		return "On a sidequest · " + facts.Sidequest.Title, contract.ActivityOnSidequest
	case !friendsSince.IsZero() && now.Sub(friendsSince) < justAddedFor:
		return "Just added", contract.ActivityNew
	}
	return "Open to plans", contract.ActivityFree
}

// announcePresence sends userID's current status line to each of their
// friends (friend.status), rendered per friend since "Just added" depends
// on the friendship. only limits the recipients when non-empty.
func (h *H) announcePresence(ctx context.Context, userID string, tz *time.Location, only ...string) error {
	return announcePresence(ctx, h.d, userID, tz, only...)
}

func announcePresence(ctx context.Context, d *api.Deps, userID string, tz *time.Location, only ...string) error {
	user, err := d.Store.Users().ByID(ctx, userID)
	if err != nil {
		return err
	}
	friendships, err := d.Store.Friends().Of(ctx, userID)
	if err != nil || len(friendships) == 0 {
		return err
	}
	now := d.Clock().In(tz)
	facts, err := d.Store.Users().Presence(ctx, []string{userID}, now)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, id := range only {
		wanted[id] = true
	}
	byLine := map[string][]string{}
	var lines []string
	for _, fs := range friendships {
		friend := store.FriendshipOther(fs, userID)
		if len(wanted) > 0 && !wanted[friend] {
			continue
		}
		line, _ := statusLine(user, facts[userID], fs.CreatedAt, now)
		if _, seen := byLine[line]; !seen {
			lines = append(lines, line)
		}
		byLine[line] = append(byLine[line], friend)
	}
	for _, line := range lines {
		realtime.FriendStatus(d.Publish(), byLine[line], userID, line)
	}
	return nil
}

// StatusChanged is PATCH /me's hook after the user's status changed: their
// friends get the new status line (friend.status), and feeds refresh when a
// live free post's audience changed with it. It never fails the request;
// errors are logged.
func StatusChanged(ctx context.Context, d *api.Deps, user *models.User, tz *time.Location) {
	ctx, cancel := eventContext(ctx)
	defer cancel()
	userID := user.ID.Hex()
	logEventError(realtime.EventFriendStatus, announcePresence(ctx, d, userID, tz))
	if _, err := d.Store.Forum().LiveFreePost(ctx, userID, d.Clock()); err == nil {
		realtime.ForumUpdate(d.Publish())
	}
}

// eventContext detaches event work from the request (a client that hangs
// up after the write still gets its events out) with a bound.
func eventContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
}
