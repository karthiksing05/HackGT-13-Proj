package social

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"context"
	"errors"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Sentences of the Forum.
const (
	MsgForumFilters  = "Check the filters and try again."
	MsgPostPlan      = "Share a plan by changing its sidequest's visibility."
	MsgPostType      = "Only free-now posts can be made here."
	MsgPostAudience  = "Pick who can see your post."
	MsgPostUntilPast = "Pick a time that hasn't passed yet."
	MsgPostUntilLong = "A free-now post can last up to 24 hours."
	MsgPostRadius    = "Pick a radius between 1 and 50 miles."
	MsgPostWhere     = "Pick an area for your post."
	MsgPlanPostOwn   = "Change the sidequest's visibility instead."
)

const (
	milesPerKm      = 0.621371
	defaultRadiusMi = 2.0
	maxRadiusMi     = 50
	forumPageSize   = 50
	forumCandidates = 500
	freeNowDefault  = 3 * time.Hour
	freeNowMax      = 24 * time.Hour
	searchRadiusMi  = 10.0
)

// forumQuery is GET /forum/posts' parameters (the app's ForumQuery).
type forumQuery struct {
	center      *travel.Point
	radiusMi    float64
	friendsOnly bool   // scope=friends
	kind        string // all | plans | free_now
	when        string // any | now | today | weekend
	maxDist     *float64
	cost        map[int]bool
	tags        []string
	openOnly    bool
	sort        string // soonest | closest | spots | newest | for_you
}

// parseForumQuery reads the query string; without lat/lng the viewer's home
// base (or last known location) is the center.
func parseForumQuery(r *http.Request, viewer *models.User) (forumQuery, error) {
	v := r.URL.Query()
	bad := httpx.BadRequest(MsgForumFilters)
	q := forumQuery{radiusMi: defaultRadiusMi, kind: "all", when: "any", sort: "soonest", cost: map[int]bool{}}
	pick := func(key string, allowed ...string) (string, bool) {
		val := strings.TrimSpace(v.Get(key))
		if val == "" {
			return "", true
		}
		if slices.Contains(allowed, val) {
			return val, true
		}
		return "", false
	}
	var ok bool
	var scope, kind, when, sortBy string
	if scope, ok = pick("scope", "everyone", "friends"); !ok {
		return q, bad
	}
	q.friendsOnly = scope == "friends"
	if kind, ok = pick("type", "all", "plans", "free_now"); !ok {
		return q, bad
	}
	if kind != "" {
		q.kind = kind
	}
	if when, ok = pick("when", "any", "now", "today", "weekend"); !ok {
		return q, bad
	}
	if when != "" {
		q.when = when
	}
	if sortBy, ok = pick("sort", "soonest", "closest", "spots", "newest", "for_you"); !ok {
		return q, bad
	}
	if sortBy != "" {
		q.sort = sortBy
	}
	num := func(key string) (*float64, error) {
		s := strings.TrimSpace(v.Get(key))
		if s == "" {
			return nil, nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, bad
		}
		return &f, nil
	}
	lat, err := num("lat")
	if err != nil {
		return q, err
	}
	lng, err := num("lng")
	if err != nil {
		return q, err
	}
	switch {
	case lat != nil && lng != nil:
		if *lat < -90 || *lat > 90 || *lng < -180 || *lng > 180 {
			return q, bad
		}
		q.center = &travel.Point{Lat: *lat, Lng: *lng}
	case lat != nil || lng != nil:
		return q, bad
	default:
		q.center = homePoint(viewer)
	}
	radius, err := num("radius")
	if err != nil {
		return q, err
	}
	if radius != nil {
		if *radius <= 0 {
			return q, bad
		}
		q.radiusMi = math.Min(*radius, maxRadiusMi)
	}
	if q.maxDist, err = num("max_dist"); err != nil {
		return q, err
	}
	for _, part := range splitList(v.Get("cost")) {
		tier, err := strconv.Atoi(part)
		if err != nil || tier < 0 || tier > 3 {
			return q, bad
		}
		q.cost[tier] = true
	}
	q.tags = splitList(v.Get("tags"))
	if s := strings.TrimSpace(v.Get("open_only")); s != "" {
		if q.openOnly, err = strconv.ParseBool(s); err != nil {
			return q, bad
		}
	}
	return q, nil
}

func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// homePoint is the viewer's home base, else their last planning location.
func homePoint(u *models.User) *travel.Point {
	if u.HomeBase != nil {
		return &travel.Point{Lat: u.HomeBase.Lat, Lng: u.HomeBase.Lng}
	}
	if u.LastLocation != nil && len(u.LastLocation.Coordinates) == 2 {
		return &travel.Point{Lat: u.LastLocation.Coordinates[1], Lng: u.LastLocation.Coordinates[0]}
	}
	return nil
}

// feed renders every post the viewer may see under q's scope, type and
// radius (filters and sort are applied by the caller). A post is visible
// when it is not the viewer's own and either its author is the viewer's
// friend (any distance) or it is open to everyone and within the radius.
// Free-now posts also respect their author's status (busy hides them,
// friends only hides them from non-friends) and the post's own radius.
func (h *H) feed(ctx context.Context, viewer *models.User, q forumQuery, tz *time.Location) ([]feedItem, error) {
	st := h.d.Store
	viewerID := viewer.ID.Hex()
	now := h.d.BusinessNow(ctx).In(tz)
	friendIDs, err := st.Friends().IDs(ctx, viewerID)
	if err != nil {
		return nil, err
	}
	friends := map[string]bool{}
	for _, id := range friendIDs {
		friends[id] = true
	}
	var free []*models.ForumPost
	if q.kind != "plans" {
		fq := store.FreePostQuery{ViewerID: viewerID, FriendIDs: friendIDs, FriendsOnly: q.friendsOnly, RadiusMi: q.radiusMi}
		if q.center != nil {
			fq.Lat, fq.Lng = &q.center.Lat, &q.center.Lng
		}
		if free, err = st.Forum().VisibleFreePosts(ctx, fq, now, forumCandidates); err != nil {
			return nil, err
		}
	}
	var plans []*models.Itinerary
	if q.kind != "free_now" {
		if plans, err = st.Forum().SharedPlans(ctx, viewerID, friendIDs, q.friendsOnly, now, forumCandidates); err != nil {
			return nil, err
		}
	}
	var ids, postIDs, joined []string
	for _, p := range free {
		ids = append(ids, p.AuthorID)
		postIDs = append(postIDs, p.ID)
	}
	for _, it := range plans {
		ids = append(ids, it.MemberIDs...)
		ids = append(ids, it.HostID)
		postIDs = append(postIDs, it.ID)
		if it.IsMember(viewerID) {
			joined = append(joined, it.ID)
		}
	}
	ppl, err := h.loadPeople(ctx, ids)
	if err != nil {
		return nil, err
	}
	sent, err := st.PlanTogether().Sent(ctx, viewerID, postIDs)
	if err != nil {
		return nil, err
	}
	groups, err := st.Threads().ByItinerary(ctx, joined)
	if err != nil {
		return nil, err
	}
	threads := map[string]string{}
	for itID, th := range groups {
		threads[itID] = th.ID
	}
	v := &postView{viewerID: viewerID, friends: friends, center: q.center, now: now, ppl: ppl, sent: sent, threads: threads}

	items := make([]feedItem, 0, len(free)+len(plans))
	for _, p := range free {
		author, ok := ppl.byID[p.AuthorID]
		if !ok || !freePostVisible(p, author, friends[p.AuthorID], v) {
			continue
		}
		if item, ok := v.freePost(p); ok {
			items = append(items, item)
		}
	}
	for _, it := range plans {
		if !friends[it.HostID] {
			mi, ok := v.distance(planPoint(it))
			if it.Visibility != models.VisibilityOpen || !ok || mi > q.radiusMi {
				continue
			}
		}
		if item, ok := v.planPost(it); ok {
			items = append(items, item)
		}
	}
	return items, nil
}

// freePostVisible applies the author's status and the post's own audience
// radius on top of the query's scope.
func freePostVisible(p *models.ForumPost, author *models.User, isFriend bool, v *postView) bool {
	switch author.Status {
	case string(contract.StatusBusy):
		return false
	case string(contract.StatusFriendsOnly):
		if !isFriend {
			return false
		}
	}
	if isFriend {
		return true
	}
	if p.Visibility != models.PostVisibilityEveryone {
		return false
	}
	mi, ok := v.distance(postPoint(p))
	if !ok {
		return false
	}
	return p.RadiusMi == nil || mi <= float64(*p.RadiusMi)
}

// keep applies the filters (MockAPIClient.forumPosts).
func (q forumQuery) keep(item feedItem) bool {
	p := item.post
	switch {
	case q.kind == "plans" && p.Type != contract.PostPlan,
		q.kind == "free_now" && p.Type != contract.PostFreeNow,
		q.friendsOnly && !p.IsFriend,
		q.when == "now" && p.StartsInMinutes > 60,
		q.when == "today" && p.Day != "today",
		q.when == "weekend" && !item.weekend,
		q.maxDist != nil && (!item.hasDist || p.DistanceMi > *q.maxDist),
		len(q.cost) > 0 && !q.cost[p.PriceTier],
		len(q.tags) > 0 && !anyTag(p.Tags, q.tags),
		q.openOnly && !(p.Type == contract.PostPlan && item.open):
		return false
	}
	return true
}

func anyTag(have, want []string) bool {
	for _, a := range have {
		for _, b := range want {
			if strings.EqualFold(a, b) {
				return true
			}
		}
	}
	return false
}

// sortFeed orders posts by the chosen key; ties go to the newer post.
// "spots": the most spots left first, unlimited plans ahead of all, free-now
// posts last. "for_you": the best taste match first; posts without one after
// the scored ones, soonest first among themselves.
func sortFeed(items []feedItem, by string) {
	if by == "for_you" {
		sort.SliceStable(items, func(i, j int) bool {
			ci, cj := items[i].post.Compatibility, items[j].post.Compatibility
			switch {
			case ci != nil && cj != nil && *ci != *cj:
				return *ci > *cj
			case (ci == nil) != (cj == nil):
				return ci != nil
			}
			return items[i].post.StartsInMinutes < items[j].post.StartsInMinutes
		})
		return
	}
	key := func(it feedItem) float64 {
		p := it.post
		switch by {
		case "closest":
			if !it.hasDist {
				return math.Inf(1)
			}
			return p.DistanceMi
		case "spots":
			switch {
			case p.Type != contract.PostPlan:
				return 1
			case p.SpotsLeft == nil:
				return math.Inf(-1)
			}
			return -float64(*p.SpotsLeft)
		case "newest":
			return float64(p.PostedMinutesAgo)
		}
		return float64(p.StartsInMinutes)
	}
	sort.SliceStable(items, func(i, j int) bool {
		ki, kj := key(items[i]), key(items[j])
		if ki != kj {
			return ki < kj
		}
		if !items[i].postedAt.Equal(items[j].postedAt) {
			return items[i].postedAt.After(items[j].postedAt)
		}
		return items[i].post.ID < items[j].post.ID
	})
}

// ListPosts is GET /forum/posts → {items: [ForumPost], next_cursor} (50 a page).
func (h *H) ListPosts(w http.ResponseWriter, r *http.Request) {
	viewer, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	q, err := parseForumQuery(r, viewer)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	items, err := h.feed(r.Context(), viewer, q, httpx.TZ(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	kept := items[:0]
	for _, it := range items {
		if q.keep(it) {
			kept = append(kept, it)
		}
	}
	h.scorePlans(r.Context(), viewer, kept)
	sortFeed(kept, q.sort)
	offset, _ := strconv.Atoi(httpx.Cursor(r))
	offset = min(max(0, offset), len(kept))
	end := min(offset+forumPageSize, len(kept))
	page := make([]contract.ForumPost, 0, end-offset)
	for _, it := range kept[offset:end] {
		page = append(page, it.post)
	}
	next := ""
	if end < len(kept) {
		next = strconv.Itoa(end)
	}
	httpx.JSON(w, http.StatusOK, contract.NewPage(page, httpx.NextCursor(next)))
}

// SearchPosts is the posts part of GET /search (backend-contract §4 B): the
// forum posts the viewer may see (friends' anywhere, everyone else's within
// 10 miles of their home base) whose title, text or author name contains q,
// soonest first.
func SearchPosts(ctx context.Context, d *api.Deps, viewer *models.User, q string, tz *time.Location) ([]contract.ForumPost, error) {
	needle := strings.ToLower(strings.TrimSpace(q))
	if needle == "" {
		return []contract.ForumPost{}, nil
	}
	h := &H{d: d}
	query := forumQuery{center: homePoint(viewer), radiusMi: searchRadiusMi, kind: "all", when: "any", sort: "soonest"}
	items, err := h.feed(ctx, viewer, query, tz)
	if err != nil {
		return nil, err
	}
	sortFeed(items, query.sort)
	out := []contract.ForumPost{}
	for _, it := range items {
		p := it.post
		text := ""
		if p.Title != nil {
			text += *p.Title + "\n"
		}
		if p.Text != nil {
			text += *p.Text + "\n"
		}
		if strings.Contains(strings.ToLower(text+p.Author.Name), needle) {
			out = append(out, p)
		}
	}
	return out, nil
}

// MyPost is GET /forum/posts/mine → MyFreePost, or 204 without a live one.
func (h *H) MyPost(w http.ResponseWriter, r *http.Request) {
	post, err := h.d.Store.Forum().LiveFreePost(r.Context(), api.UserID(r), h.d.BusinessNow(r.Context()))
	if errors.Is(err, store.ErrNotFound) {
		httpx.NoContent(w)
		return
	}
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, myPostView(post))
}

// CreatePost is POST /forum/posts (type free_now) → 201 MyFreePost. It
// replaces the viewer's live post; until defaults to 3 hours from now and
// the place to their home base. Friends hear the new status line.
func (h *H) CreatePost(w http.ResponseWriter, r *http.Request) {
	var req contract.NewForumPost
	if !httpx.Decode(w, r, &req) {
		return
	}
	switch req.Type {
	case contract.PostFreeNow:
	case contract.PostPlan:
		httpx.Error(w, http.StatusBadRequest, MsgPostPlan)
		return
	default:
		httpx.Error(w, http.StatusBadRequest, MsgPostType)
		return
	}
	if !req.Visibility.Valid() {
		httpx.Error(w, http.StatusBadRequest, MsgPostAudience)
		return
	}
	viewer, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	tz := httpx.TZ(r)
	now := h.d.BusinessNow(r.Context())
	until := now.Add(freeNowDefault).Truncate(time.Minute)
	if req.Until != nil {
		until = req.Until.Time
	}
	switch {
	case !until.After(now):
		httpx.Error(w, http.StatusBadRequest, MsgPostUntilPast)
		return
	case until.Sub(now) > freeNowMax:
		httpx.Error(w, http.StatusBadRequest, MsgPostUntilLong)
		return
	case req.RadiusMi != nil && (*req.RadiusMi < 1 || *req.RadiusMi > maxRadiusMi):
		httpx.Error(w, http.StatusBadRequest, MsgPostRadius)
		return
	}
	var at *travel.Point
	switch {
	case req.Lat != nil && req.Lng != nil:
		if *req.Lat < -90 || *req.Lat > 90 || *req.Lng < -180 || *req.Lng > 180 {
			httpx.Error(w, http.StatusBadRequest, MsgPostWhere)
			return
		}
		at = &travel.Point{Lat: *req.Lat, Lng: *req.Lng}
	default:
		at = homePoint(viewer)
	}
	if at == nil {
		httpx.Error(w, http.StatusBadRequest, MsgPostWhere)
		return
	}
	var area *string
	if req.AreaLabel != nil {
		if label := strings.TrimSpace(*req.AreaLabel); label != "" {
			area = &label
		}
	}
	near := "you"
	if area != nil && *area != "Current location" {
		near = *area
	}
	post := &models.ForumPost{
		AuthorID: viewer.ID.Hex(), Visibility: string(req.Visibility),
		Location:  models.GeoJSONPoint{Type: "Point", Coordinates: []float64{at.Lng, at.Lat}},
		AreaLabel: area, RadiusMi: req.RadiusMi, Until: until,
		Text: "Free until " + httpx.Clock(until.In(tz)) + " near " + near,
	}
	if err := h.d.Store.Forum().ReplaceFreePost(r.Context(), post); err != nil {
		api.Fail(w, r, err)
		return
	}
	realtime.ForumUpdate(h.d.Publish())
	h.announce(r.Context(), post.AuthorID, tz)
	httpx.JSON(w, http.StatusCreated, myPostView(post))
}

// DeletePost is DELETE /forum/posts/{id}: the author takes their free-now
// post down → 204. A plan post is a sidequest: its host changes the
// visibility instead (400).
func (h *H) DeletePost(w http.ResponseWriter, r *http.Request) {
	viewerID := api.UserID(r)
	id := mux.Vars(r)["id"]
	ctx := r.Context()
	err := h.d.Store.Forum().DeleteFreePost(ctx, viewerID, id)
	if errors.Is(err, store.ErrNotFound) {
		if it, planErr := h.d.Store.Forum().SharedPlan(ctx, id); planErr == nil && it.HostID == viewerID {
			httpx.Error(w, http.StatusBadRequest, MsgPlanPostOwn)
			return
		}
	}
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	realtime.ForumUpdate(h.d.Publish())
	h.announce(ctx, viewerID, httpx.TZ(r))
	httpx.NoContent(w)
}

// announce sends the user's status line to their friends, logging failures.
func (h *H) announce(ctx context.Context, userID string, tz *time.Location) {
	ctx, cancel := eventContext(ctx)
	defer cancel()
	logEventError(realtime.EventFriendStatus, h.announcePresence(ctx, userID, tz))
}
