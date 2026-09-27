package social

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

const (
	// profilePlans caps the plans a profile lists, picked from the first
	// profilePlanPool shared ones (a stranger doesn't see friends-only ones).
	profilePlans    = 10
	profilePlanPool = 50
	// profileDoneCap bounds the past sidequests a profile counts.
	profileDoneCap = 500
	profileMutual  = 3 // friends in common shown by name
	profileLikes   = 5
	profileReasons = 3
)

// Liking a trip type blends what someone said in Setup (a 1–5 rating over
// 5) with what their ratings of past stops taught us (users.taste.tags,
// 0…1), weighted as Account › Taste weighs them; either reads 0.5 when
// missing. likeFloor or more is a like: a 4 on its own, or a 3 that their
// ratings back up.
const (
	likeSaid    = 0.6
	likeLearned = 0.4
	likeNeutral = 0.5
	likeFloor   = 0.65
)

// tripTypeLabels name the trip types as Setup does ("What do you enjoy?").
var tripTypeLabels = map[string]string{
	"outdoors": "Outdoors & parks", "food": "Food & drinks", "museums": "Museums & art", "live_music": "Live music",
	"nightlife": "Nightlife", "sports": "Sports & games", "shopping": "Shopping & markets", "big_crowds": "Big crowds",
	"early_mornings": "Early mornings", "long_walks": "Long walks",
}

// bothLike is the match reason for a trip type two people both like.
var bothLike = map[string]string{
	"outdoors": "You both love the outdoors", "food": "You're both foodies", "museums": "You both love museums and art",
	"live_music": "You both love live music", "nightlife": "You both love a night out",
	"sports": "You're both into sports and games", "shopping": "You both love markets and shopping",
	"big_crowds": "You both like a big crowd", "early_mornings": "You're both early risers",
	"long_walks": "You both love long walks",
}

// Profile is GET /users/{id}/profile → PublicProfile: who someone is to the
// viewer, 404 for an unknown id. It keeps the Forum's rules: their friends
// see their status line and friends-only plans, everyone else only their
// open plans, and a minor's school stays with their friends. The taste
// match is best effort: absent on your own profile, without both taste
// vectors, across catalogs, or while the ML service is down.
func (h *H) Profile(w http.ResponseWriter, r *http.Request) {
	viewer, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	person, err := h.d.Store.Users().ByID(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out, err := h.profile(r.Context(), viewer, person, httpx.TZ(r))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// profile renders person's profile for viewer (tz is the viewer's zone).
func (h *H) profile(ctx context.Context, viewer, person *models.User, tz *time.Location) (contract.PublicProfile, error) {
	st := h.d.Store
	viewerID, personID := viewer.ID.Hex(), person.ID.Hex()
	now := h.d.BusinessNow(ctx).In(tz)
	self := viewerID == personID
	likes := liked(person)
	out := contract.PublicProfile{
		Person: view.PersonRef(person, h.d.Cfg.PublicBaseURL), City: view.StrPtr(person.City),
		Status: presence(person.Status), Relation: contract.ProfileSelf,
		MatchReasons: []string{}, Likes: likeLabels(likes),
		MutualFriends: contract.MutualFriends{People: []contract.PersonRef{}}, OpenPlans: []contract.ForumPost{},
	}
	done, err := st.Itineraries().ListPast(ctx, personID, now, profileDoneCap)
	if err != nil {
		return out, err
	}
	out.SidequestsDone = len(done)
	friendIDs, err := st.Friends().IDs(ctx, viewerID)
	if err != nil {
		return out, err
	}
	isFriend := slices.Contains(friendIDs, personID)
	var plans []*models.Itinerary
	if self {
		plans, err = h.ownPlans(ctx, viewerID, now)
	} else {
		if err := h.relate(ctx, &out, viewerID, person, friendIDs, isFriend, now); err != nil {
			return out, err
		}
		out.MatchReasons = matchReasons(liked(viewer), likes)
		out.Compatibility = h.personMatch(ctx, viewer, person)
		plans, err = h.visiblePlans(ctx, viewerID, personID, isFriend, now)
	}
	if err != nil {
		return out, err
	}
	if self || isFriend || !isMinor(person, now) {
		out.School = nonEmpty(person.School)
	}
	out.OpenPlans, err = h.planPosts(ctx, viewer, friendIDs, plans, now)
	return out, err
}

// relate fills in how person relates to the viewer: the relation (with the
// pending request), their friends in common, and a friend's status line.
func (h *H) relate(ctx context.Context, out *contract.PublicProfile, viewerID string, person *models.User,
	friendIDs []string, isFriend bool, now time.Time) error {
	st := h.d.Store
	personID := person.ID.Hex()
	relations, err := st.Friends().Relations(ctx, viewerID, []string{personID})
	if err != nil {
		return err
	}
	rel := relations[personID]
	out.Relation = contract.ProfileRelation(rel.Kind)
	out.RequestID = view.StrPtr(rel.RequestID)

	theirs, err := st.Friends().IDs(ctx, personID)
	if err != nil {
		return err
	}
	var common []string
	for _, id := range theirs {
		if slices.Contains(friendIDs, id) {
			common = append(common, id)
		}
	}
	ppl, err := h.loadPeople(ctx, common)
	if err != nil {
		return err
	}
	refs := ppl.refs(common, "", 0)
	sort.SliceStable(refs, func(i, j int) bool { return strings.ToLower(refs[i].Name) < strings.ToLower(refs[j].Name) })
	out.MutualFriends = contract.MutualFriends{Count: len(refs), People: refs[:min(profileMutual, len(refs))]}

	if !isFriend {
		return nil
	}
	fs, err := st.Friends().Get(ctx, viewerID, personID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return nil // unfriended just now
	case err != nil:
		return err
	}
	facts, err := st.Users().Presence(ctx, []string{personID}, now)
	if err != nil {
		return err
	}
	line, _ := statusLine(person, facts[personID], fs.CreatedAt, now)
	out.StatusLine = &line
	return nil
}

// personMatch is the taste match of viewer and person (0–100), or nil when
// there is none to show: either has no taste vector, they plan from
// different catalogs, or the ML service is missing or failing (logged).
func (h *H) personMatch(ctx context.Context, viewer, person *models.User) *int {
	if h.d.ML == nil || store.CatalogFor(viewer) != store.CatalogFor(person) ||
		!ml.Usable(viewer.PositiveEmbedding, ml.Dim) || !ml.Usable(person.PositiveEmbedding, ml.Dim) {
		return nil
	}
	id := person.ID.Hex()
	matches, err := h.d.ML.UserCompatibility(ctx, tasteVectors(viewer),
		[]ml.UserCandidate{{ID: id, Positive: person.PositiveEmbedding, Negative: person.NegativeEmbedding}})
	if err != nil {
		if !errors.Is(err, ml.ErrNoUserEmbedding) {
			log.Warn().Err(err).Msg("profile match: ML service failed")
		}
		return nil
	}
	for _, m := range matches {
		if m.ID == id {
			percent := min(100, max(0, m.Percent))
			return &percent
		}
	}
	return nil
}

// visiblePlans are the Forum plans person hosts that the viewer may see,
// soonest first: all of a friend's, only the open ones of anyone else.
func (h *H) visiblePlans(ctx context.Context, viewerID, personID string, isFriend bool, now time.Time) ([]*models.Itinerary, error) {
	// With person as the only "friend", the Forum's friends-only query is
	// exactly the plans they host; who may see which is decided here.
	shared, err := h.d.Store.Forum().SharedPlans(ctx, viewerID, []string{personID}, true, now, profilePlanPool)
	if err != nil {
		return nil, err
	}
	plans := []*models.Itinerary{}
	for _, it := range shared {
		if (isFriend || it.Visibility == models.VisibilityOpen) && len(plans) < profilePlans {
			plans = append(plans, it)
		}
	}
	return plans, nil
}

// ownPlans are the viewer's upcoming plans shared to the Forum (what others
// may see on their profile), soonest first.
func (h *H) ownPlans(ctx context.Context, viewerID string, now time.Time) ([]*models.Itinerary, error) {
	active, err := h.d.Store.Itineraries().ListActive(ctx, viewerID, now, profilePlanPool)
	if err != nil {
		return nil, err
	}
	plans := []*models.Itinerary{}
	for _, it := range active {
		if it.HostID == viewerID && it.Visibility != models.VisibilityJustMe && len(plans) < profilePlans {
			plans = append(plans, it)
		}
	}
	return plans, nil
}

// planPosts renders plans as the Forum renders plan posts for the viewer:
// their join status (and group chat once in), the distance from their home
// base and the plan's taste match.
func (h *H) planPosts(ctx context.Context, viewer *models.User, friendIDs []string, plans []*models.Itinerary, now time.Time) ([]contract.ForumPost, error) {
	out := []contract.ForumPost{}
	if len(plans) == 0 {
		return out, nil
	}
	st := h.d.Store
	viewerID := viewer.ID.Hex()
	friends := make(map[string]bool, len(friendIDs))
	for _, id := range friendIDs {
		friends[id] = true
	}
	var ids, postIDs, joined []string
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
	threads := make(map[string]string, len(groups))
	for itID, th := range groups {
		threads[itID] = th.ID
	}
	v := &postView{viewerID: viewerID, friends: friends, center: homePoint(viewer), now: now, ppl: ppl, sent: sent, threads: threads}
	items := make([]feedItem, 0, len(plans))
	for _, it := range plans {
		if item, ok := v.planPost(it); ok {
			items = append(items, item)
		}
	}
	h.scorePlans(ctx, viewer, items)
	for _, it := range items {
		out = append(out, it.post)
	}
	return out, nil
}

// tripLike is how much someone likes one of contract.TripTypes.
type tripLike struct {
	key   string
	score float64
}

// liked is every trip type u likes, most liked first (ties in
// contract.TripTypes order).
func liked(u *models.User) []tripLike {
	var out []tripLike
	for _, key := range contract.TripTypes {
		said, learned := likeNeutral, likeNeutral
		if v, ok := u.Prefs.Ratings[key]; ok && v >= 1 && v <= 5 {
			said = float64(v) / 5
		}
		if v, ok := u.Taste.Tags[key]; ok && !math.IsNaN(v) {
			learned = math.Max(0, math.Min(1, v))
		}
		if score := likeSaid*said + likeLearned*learned; score >= likeFloor {
			out = append(out, tripLike{key: key, score: score})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

// likeLabels are the labels of the profileLikes most liked trip types.
func likeLabels(likes []tripLike) []string {
	out := []string{}
	for _, l := range likes[:min(profileLikes, len(likes))] {
		out = append(out, tripTypeLabels[l.key])
	}
	return out
}

// matchReasons say why two people match (up to profileReasons): the trip
// types both like, the ones they like most together first.
func matchReasons(viewer, person []tripLike) []string {
	theirs := make(map[string]float64, len(person))
	for _, l := range person {
		theirs[l.key] = l.score
	}
	var both []tripLike
	for _, l := range viewer {
		if score, ok := theirs[l.key]; ok {
			both = append(both, tripLike{key: l.key, score: l.score + score})
		}
	}
	sort.SliceStable(both, func(i, j int) bool { return both[i].score > both[j].score })
	out := []string{}
	for _, l := range both[:min(profileReasons, len(both))] {
		out = append(out, bothLike[l.key])
	}
	return out
}

// presence is the account's status (open when it was never set).
func presence(status string) contract.PresenceStatus {
	if s := contract.PresenceStatus(status); s.Valid() {
		return s
	}
	return contract.StatusOpen
}

// isMinor: under 18 (their school is for their friends only).
func isMinor(u *models.User, now time.Time) bool {
	switch view.AgeBracket(u.BirthDate, now) {
	case contract.AgeUnder13, contract.AgeTeen:
		return true
	}
	return false
}

// nonEmpty is s unless it is missing or blank.
func nonEmpty(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	return s
}
