package social

import (
	"Backend/pkg/api/photos"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"context"
	"math"
	"strings"
	"time"
)

// people renders users for one response; ids of deleted accounts are
// simply missing.
type people struct {
	byID map[string]*models.User
	base string
}

// loadPeople loads every user named in ids (duplicates and "" ignored).
func (h *H) loadPeople(ctx context.Context, ids ...[]string) (people, error) {
	seen := map[string]bool{}
	var all []string
	for _, list := range ids {
		for _, id := range list {
			if id != "" && !seen[id] {
				seen[id] = true
				all = append(all, id)
			}
		}
	}
	byID, err := h.d.Store.Users().ByIDs(ctx, all)
	if err != nil {
		return people{}, err
	}
	return people{byID: byID, base: h.d.Cfg.PublicBaseURL}, nil
}

func (p people) ref(id string) (contract.PersonRef, bool) {
	u, ok := p.byID[id]
	if !ok {
		return contract.PersonRef{}, false
	}
	return view.PersonRef(u, p.base), true
}

// refs renders ids in order, leaving out skip and unknown users; limit 0
// means all. Never nil.
func (p people) refs(ids []string, skip string, limit int) []contract.PersonRef {
	out := []contract.PersonRef{}
	for _, id := range ids {
		if id == skip {
			continue
		}
		if limit > 0 && len(out) == limit {
			break
		}
		if ref, ok := p.ref(id); ok {
			out = append(out, ref)
		}
	}
	return out
}

// firstName is how chats name someone ("Maya"); "" for an unknown user.
func (p people) firstName(id string) string {
	if u, ok := p.byID[id]; ok {
		return firstWord(u.Name)
	}
	return ""
}

func firstWord(name string) string {
	if fields := strings.Fields(name); len(fields) > 0 {
		return fields[0]
	}
	return ""
}

// ---- tz-aware labels (callers pass times already in the viewer's zone) ----

// whenLabel is a plan's "Today · 5:30–8 PM".
func whenLabel(start, end, now time.Time) string {
	return httpx.DayLabel(start, now) + " · " + httpx.TimeRange(start, end)
}

// lockLabel is "Locks 5:00 PM" (today), "Locks Fri 9 PM" (this week) or
// "Locks Oct 3 9 PM"; "Locked …" once it has passed.
func lockLabel(lock, now time.Time) string {
	verb := "Locks "
	if !lock.After(now) {
		verb = "Locked "
	}
	switch diff := httpx.DayDiff(lock, now); {
	case diff == 0:
		return verb + httpx.Clock(lock)
	case diff > 0 && diff < 7:
		return verb + httpx.Weekday(lock) + " " + httpx.ClockShort(lock)
	}
	return verb + httpx.MonthDay(lock) + " " + httpx.ClockShort(lock)
}

// lastTimeLabel is a thread's "5:12 PM" (today), "Thu" (this past week) or "Sep 20".
func lastTimeLabel(t, now time.Time) string {
	switch diff := httpx.DayDiff(t, now); {
	case diff == 0:
		return httpx.Clock(t)
	case diff < 0 && diff > -7:
		return httpx.Weekday(t)
	}
	return httpx.MonthDay(t)
}

// minutesBetween is whole minutes from a to b, never negative.
func minutesBetween(a, b time.Time) int {
	return max(0, int(b.Sub(a)/time.Minute))
}

// ---- messages, photos, expenses ------------------------------------------

// messageView renders a message for one viewer: "You" for their own, the
// sender's first name otherwise. client_id echoes what a device sent; the
// server's own ("Plan together") stays off the wire.
func messageView(m *models.Message, viewerID string, ppl people) contract.Message {
	name := "You"
	if m.SenderID != viewerID {
		if name = ppl.firstName(m.SenderID); name == "" {
			name = "Someone"
		}
	}
	clientID := m.ClientID
	if strings.HasPrefix(clientID, planTogetherTag) {
		clientID = ""
	}
	return contract.Message{
		ID: m.ID, SenderID: m.SenderID, SenderName: name, Text: m.Text,
		SentAt: contract.NewTime(m.SentAt), ClientID: view.StrPtr(clientID),
	}
}

// photoView renders a group photo for one viewer ("You" added it, or a first name).
func photoView(p *models.Photo, viewerID string, ppl people) contract.GroupPhoto {
	by := "You"
	if p.OwnerID != viewerID {
		if by = ppl.firstName(p.OwnerID); by == "" {
			by = "Someone"
		}
	}
	url := photos.URL(ppl.base, p.ID)
	owner := p.OwnerID
	return contract.GroupPhoto{ID: p.ID, ByName: by, UploaderID: &owner, CreatedAt: contract.Ptr(p.CreatedAt), URL: &url}
}

func expenseView(e *models.Expense) contract.Expense {
	split, shares := e.SplitAmong, e.Shares
	if split == nil {
		split = []string{}
	}
	if len(shares) != len(split) {
		shares = equalShares(e.AmountCents, len(split))
	}
	return contract.Expense{
		ID: e.ID, What: e.What, AmountCents: e.AmountCents, PayerID: e.PayerID,
		SplitAmong: split, Shares: shares, CreatedBy: view.StrPtr(e.CreatedBy),
	}
}

// ---- friend requests ---------------------------------------------------------

// requestView renders a pending request for one side of it; false when the
// other person's account is gone.
func requestView(req *models.FriendRequest, viewerID string, ppl people, now time.Time) (contract.FriendRequest, bool) {
	outgoing := req.FromID == viewerID
	other := req.FromID
	if outgoing {
		other = req.ToID
	}
	ref, ok := ppl.ref(other)
	if !ok {
		return contract.FriendRequest{}, false
	}
	note := req.Note
	if note == "" {
		note = "Wants to be friends"
		if outgoing {
			note = "Requested " + ago(req.CreatedAt, now)
		}
	}
	return contract.FriendRequest{ID: req.ID, Person: ref, Note: note, Outgoing: outgoing}, true
}

// ago is "just now", "5 min ago", "3 h ago" or "on Sep 20".
func ago(t, now time.Time) string {
	switch d := now.Sub(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return httpx.Plural(int(d/time.Minute), "min", "min") + " ago"
	case d < 24*time.Hour:
		return httpx.Plural(int(d/time.Hour), "h", "h") + " ago"
	}
	return "on " + httpx.MonthDay(t)
}

// myPostView is the viewer's own free-now post.
func myPostView(p *models.ForumPost) contract.MyFreePost {
	until := p.Until
	return contract.MyFreePost{
		ID: p.ID, Visibility: contract.ForumPostVisibility(p.Visibility), Text: p.Text,
		Until: contract.Ptr(until), AreaLabel: p.AreaLabel, RadiusMi: p.RadiusMi,
	}
}

// joinStatus is where the viewer stands on a plan post.
func joinStatus(it *models.Itinerary, viewerID string, now time.Time) contract.JoinStatus {
	switch {
	case it.IsMember(viewerID):
		return contract.JoinJoined
	case store.JoinClosed(it, now):
		return contract.JoinClosed
	}
	if left := store.JoinSpotsLeft(it); left != nil && *left == 0 {
		return contract.JoinFull
	}
	return contract.JoinNone
}

// ---- forum posts ---------------------------------------------------------------

// postView holds what rendering a feed needs for one viewer.
type postView struct {
	viewerID string
	friends  map[string]bool
	center   *travel.Point
	now      time.Time // in the viewer's zone
	ppl      people
	sent     map[string]bool   // posts the viewer sent "Plan together" for
	threads  map[string]string // itinerary id → group thread id (plans the viewer is in)
}

// feedItem is a rendered post plus what the filters and sorts need.
type feedItem struct {
	post     contract.ForumPost
	postedAt time.Time
	weekend  bool // happens on a Saturday or Sunday
	open     bool // a plan that still takes people
	hasDist  bool
	// activityIDs are a plan's catalog stops, for its taste match.
	activityIDs []string
}

// distance is the rounded miles from the feed's center (at least 0.1 so the
// label never reads "0 mi"); false without a center or a location.
func (v *postView) distance(pt *travel.Point) (float64, bool) {
	if v.center == nil || pt == nil {
		return 0, false
	}
	mi := travel.HaversineKm(*v.center, *pt) * milesPerKm
	return max(0.1, math.Round(mi*10)/10), true
}

func withDistance(label string, mi float64, ok bool) string {
	if !ok {
		return label
	}
	return label + " · " + httpx.Miles(mi) + " away"
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

// freePost renders a free-now post: "Free now · 0.3 mi away", no tags, day today.
func (v *postView) freePost(p *models.ForumPost) (feedItem, bool) {
	author, ok := v.ppl.ref(p.AuthorID)
	if !ok {
		return feedItem{}, false
	}
	mi, hasDist := v.distance(postPoint(p))
	text := p.Text
	post := contract.ForumPost{
		ID: p.ID, Type: contract.PostFreeNow, Author: author, IsFriend: v.friends[p.AuthorID],
		FriendsOnly: p.Visibility == models.PostVisibilityFriends, Text: &text,
		Meta: withDistance("Free now", mi, hasDist), Day: "today", DistanceMi: mi,
		Tags: []string{}, Going: []contract.PersonRef{}, PostedMinutesAgo: minutesBetween(p.CreatedAt, v.now),
		JoinStatus: contract.JoinNone, PlanTogetherSent: v.sent[p.ID],
	}
	return feedItem{post: post, postedAt: p.CreatedAt, weekend: isWeekend(v.now), hasDist: hasDist}, true
}

// planPost renders an itinerary as a plan post for the viewer.
func (v *postView) planPost(it *models.Itinerary) (feedItem, bool) {
	host, ok := v.ppl.ref(it.HostID)
	if !ok {
		return feedItem{}, false
	}
	loc := v.now.Location()
	start, end := it.Start.In(loc), it.BackBy.In(loc)
	mi, hasDist := v.distance(planPoint(it))
	title := it.Title
	when := whenLabel(start, end, v.now)
	day := "today"
	if httpx.DayDiff(start, v.now) != 0 {
		day = strings.ToLower(httpx.Weekday(start))
	}
	tier, tags := 0, []string{}
	if it.Plan != nil {
		tier = min(3, max(0, it.Plan.Budget))
		if it.Plan.Tags != nil {
			tags = it.Plan.Tags
		}
	}
	status := joinStatus(it, v.viewerID, v.now)
	post := contract.ForumPost{
		ID: it.ID, Type: contract.PostPlan, Author: host, IsFriend: v.friends[it.HostID],
		FriendsOnly: it.Visibility == models.VisibilityFriends, Title: &title,
		Meta: withDistance("Hosting", mi, hasDist), When: &when, Route: routeLabel(it),
		StartsInMinutes: minutesBetween(v.now, start), Day: day, DistanceMi: mi, PriceTier: tier, Tags: tags,
		SpotsLeft: store.JoinSpotsLeft(it), Capacity: it.MaxGroupSize,
		Going: v.ppl.refs(it.MemberIDs, it.HostID, 3), GoingCount: len(it.MemberIDs),
		PostedMinutesAgo: minutesBetween(it.CreatedAt, v.now), JoinStatus: status, PlanTogetherSent: v.sent[it.ID],
	}
	if it.LockAt != nil {
		label := lockLabel(it.LockAt.In(loc), v.now)
		post.LockLabel = &label
	}
	if status == contract.JoinJoined {
		post.ThreadID = view.StrPtr(v.threads[it.ID])
	}
	open := !store.JoinClosed(it, v.now) && (post.SpotsLeft == nil || *post.SpotsLeft > 0)
	return feedItem{post: post, postedAt: it.CreatedAt, weekend: isWeekend(start), open: open, hasDist: hasDist,
		activityIDs: stopActivityIDs(it)}, true
}

// stopActivityIDs are the catalog ids of an itinerary's stops, each once.
func stopActivityIDs(it *models.Itinerary) []string {
	seen := map[string]bool{}
	var ids []string
	for _, item := range it.Items {
		if item.Kind == models.ItemTransit || item.ActivityID == "" || seen[item.ActivityID] {
			continue
		}
		seen[item.ActivityID] = true
		ids = append(ids, item.ActivityID)
	}
	return ids
}

// routeLabel is "3 stops · walking" from the plan's legs (most common mode),
// else its route mode or range; nil for a plan without stops.
func routeLabel(it *models.Itinerary) *string {
	stops := 0
	counts := map[string]int{}
	best := ""
	for _, item := range it.Items {
		if item.Kind != models.ItemTransit {
			if item.Kind != "busy" { // the host's calendar, not a stop
				stops++
			}
			continue
		}
		if item.LegMode == "" {
			continue
		}
		counts[item.LegMode]++
		if best == "" || counts[item.LegMode] > counts[best] {
			best = item.LegMode
		}
	}
	if stops == 0 {
		return nil
	}
	if best == "" {
		best = it.RouteMode
	}
	if best == "" && it.Plan != nil {
		best = it.Plan.Range
	}
	label := httpx.Plural(stops, "stop", "stops") + " · " + modeWord(best)
	return &label
}

// modeWord names how a plan gets around: walking, MARTA (the transit label) or driving.
func modeWord(mode string) string {
	switch mode {
	case "marta", "transit":
		return "MARTA"
	case "drive", "rideshare", "uber", "anywhere":
		return "driving"
	}
	return "walking"
}

// postPoint is a free post's location.
func postPoint(p *models.ForumPost) *travel.Point {
	if len(p.Location.Coordinates) != 2 {
		return nil
	}
	return &travel.Point{Lat: p.Location.Coordinates[1], Lng: p.Location.Coordinates[0]}
}

// planPoint is where a plan meets: its start place, else its first stop
// with a coordinate; nil when it has none.
func planPoint(it *models.Itinerary) *travel.Point {
	if it.StartPlace.HasCoordinate() {
		return &travel.Point{Lat: *it.StartPlace.Lat, Lng: *it.StartPlace.Lng}
	}
	for _, item := range it.Items {
		if item.Kind != models.ItemTransit && item.Place != nil && item.Place.HasCoordinate() {
			return &travel.Point{Lat: *item.Place.Lat, Lng: *item.Place.Lng}
		}
	}
	return nil
}

// ---- chat threads ----------------------------------------------------------------

// threadKit holds everything needed to render a set of threads for any of
// their members (responses and per-recipient thread.updated events).
type threadKit struct {
	ppl      people
	plans    map[string]*models.Itinerary   // group threads' itineraries
	photos   map[string]int                 // group id → photo count
	expenses map[string][]*models.Expense   // group id → expenses
	pairs    map[string]*models.Friendship  // DM key → friendship
	presence map[string]store.PresenceFacts // DM members' presence
	now      time.Time                      // in the viewer's zone
}

// render builds a thread as viewerID sees it.
func (k *threadKit) render(th *models.Thread, viewerID string) contract.ChatThread {
	out := contract.ChatThread{
		ID: th.ID, IsGroup: th.IsGroup, Members: k.ppl.refs(th.MemberIDs, "", 0), Chips: []string{},
		Unread: th.Unread[viewerID], LastMessage: k.lastMessage(th, viewerID),
	}
	if th.LastSenderID != "" {
		out.LastTime = lastTimeLabel(th.LastMessageAt.In(k.now.Location()), k.now)
	}
	if th.IsGroup {
		k.group(&out, th, viewerID)
	} else {
		k.dm(&out, th, viewerID)
	}
	return out
}

// lastMessage is "You: …" or "Maya: …" ("" before the first message).
func (k *threadKit) lastMessage(th *models.Thread, viewerID string) string {
	switch {
	case th.LastSenderID == "":
		return ""
	case th.LastSenderID == viewerID:
		return "You: " + th.LastMessageText
	}
	if name := k.ppl.firstName(th.LastSenderID); name != "" {
		return name + ": " + th.LastMessageText
	}
	return th.LastMessageText
}

// group fills title (the itinerary's), subtitle ("3 people · Today 6 PM"),
// faces, the chips (people, balance, photos) and the album lines.
func (k *threadKit) group(out *contract.ChatThread, th *models.Thread, viewerID string) {
	loc := k.now.Location()
	it := k.plans[th.ItineraryID]
	switch {
	case it != nil && it.Title != "":
		out.Title = it.Title // follows a rename by the host
	case th.Title != "":
		out.Title = th.Title
	default:
		out.Title = "Group chat"
	}
	count := httpx.Plural(len(out.Members), "person", "people")
	out.Subtitle = count
	albumDay := th.CreatedAt
	if it != nil {
		start := it.Start.In(loc)
		out.Subtitle += " · " + httpx.DayLabel(start, k.now) + " " + httpx.ClockShort(start)
		albumDay = it.Start
	}
	out.Faces = k.ppl.refs(th.MemberIDs, viewerID, 2)
	net := 0
	for _, b := range balances(k.expenses[th.ID], viewerID, th.MemberIDs) {
		net += b.NetCents
	}
	photoCount := httpx.Plural(k.photos[th.ID], "photo", "photos")
	out.Chips = []string{count, balanceChip(net), photoCount}
	albumTitle := out.Title + " · " + httpx.MonthDay(albumDay.In(loc))
	albumSubtitle := photoCount + " · " + count
	out.AlbumTitle, out.AlbumSubtitle = &albumTitle, &albumSubtitle
}

// balanceChip is "You owe $9", "You're owed $4" or "Settled up".
func balanceChip(net int) string {
	switch {
	case net < 0:
		return "You owe " + httpx.Dollars(-net)
	case net > 0:
		return "You're owed " + httpx.Dollars(net)
	}
	return "Settled up"
}

// dm fills a two-person thread: the other person's name, and their status
// line when you are friends ("From the Forum" otherwise).
func (k *threadKit) dm(out *contract.ChatThread, th *models.Thread, viewerID string) {
	other := ""
	for _, id := range th.MemberIDs {
		if id != viewerID {
			other = id
			break
		}
	}
	out.Faces = []contract.PersonRef{}
	out.Title = "SideQuests member"
	if ref, ok := k.ppl.ref(other); ok {
		out.Title = ref.Name
		out.Faces = []contract.PersonRef{ref}
	}
	out.Subtitle = "From the Forum"
	if fs := k.pairs[models.FriendshipID(viewerID, other)]; fs != nil {
		if u, ok := k.ppl.byID[other]; ok {
			out.Subtitle, _ = statusLine(u, k.presence[other], fs.CreatedAt, k.now)
		}
	}
}

// friendView renders a friend with their status line.
func friendView(u *models.User, facts store.PresenceFacts, since time.Time, now time.Time, base string) contract.Friend {
	line, activity := statusLine(u, facts, since, now)
	return contract.Friend{Person: view.PersonRef(u, base), StatusLine: line, Activity: activity}
}
