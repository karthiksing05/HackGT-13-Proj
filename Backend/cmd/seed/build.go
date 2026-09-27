package main

import (
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Every document the seed writes carries seed: seedTag and the world it
// belongs to, and has a fixed id, so a rerun rewrites the same documents
// and --remove finds exactly them.
const (
	seedTag    = "showcase-v1"
	fieldSeed  = "seed"
	fieldWorld = "seedWorld"
)

// action is what the seed does with one document.
type action int

const (
	actCreate   action = iota
	actUpdate          // the seed's own document, rewritten (times refreshed)
	actKeep            // someone else's document already says it: left alone
	actConflict        // someone else's document is in the way: nothing is written
)

// change is one document the seed wants, with what it will do about it.
type change struct {
	coll   string
	id     any
	act    action
	label  string
	detail string // a second report line (a plan's route)
	note   string // why it is kept or in conflict
	doc    any
}

// section is one group of changes in the report.
type section struct {
	title   string
	changes []change
	extra   string
}

// worldClock is a world's time. Saltlight lives on the demo date at the
// real time of day (pkg/democlock); now is that business time, real the
// wall clock, which TTL expiries and account metadata use.
type worldClock struct {
	now, real time.Time
	loc       *time.Location
}

// expiry is the wall-clock instant of a business deadline (democlock.RealAt).
func (c worldClock) expiry(t time.Time) time.Time { return t.Add(c.real.Sub(c.now)) }

// storedUser is a users document with the seed's tag.
type storedUser struct {
	models.User `bson:",inline"`
	Seed        string `bson:"seed,omitempty"`
}

// member is one person of the cast as this run resolves them.
type member struct {
	person
	uid     bson.ObjectID
	act     action
	note    string
	current *models.User // the stored account (update, keep)
	// how their taste vectors come out: through the ML service, or the blend
	taste    string
	pos, neg []float64
}

// worldPlan is everything the seed would write for one world.
type worldPlan struct {
	spec     *worldSpec
	short    string
	clock    worldClock
	skipped  string
	catalog  *catalog
	sandy    *models.User
	members  []*member
	ids      map[string]string // cast key (or sandyKey) → user id
	names    map[string]string // user id → name
	vectors  []catalogVector
	sections []*section
	threads  []string // group chats written (their last message is set after the messages)
	notes    []string
}

func shortName(world string) string {
	if world == worldSaltlight {
		return "slt"
	}
	return "atl"
}

// planWorld reads what the world needs and works out every change.
func planWorld(ctx context.Context, st *store.Store, w *worldSpec, o Options, taste tasteSource) (*worldPlan, error) {
	wp := &worldPlan{spec: w, short: shortName(w.name), ids: map[string]string{}, names: map[string]string{}}
	wp.clock = worldClock{now: o.Now, real: o.Now, loc: o.Atlanta}
	if w.name == worldSaltlight {
		wp.clock = worldClock{now: o.Demo.Shift(o.Now), real: o.Now, loc: o.Demo.Location()}
		sandy, why, err := findSandy(ctx, st, o.DemoEmail)
		if err != nil {
			return nil, err
		}
		if why != "" {
			wp.skipped = why
			return wp, nil
		}
		wp.sandy = sandy
		wp.ids[sandyKey] = sandy.ID.Hex()
		wp.names[sandy.ID.Hex()] = sandy.Name
	}
	var err error
	if wp.catalog, err = loadCatalog(ctx, st, w.catalog); err != nil {
		return nil, err
	}
	if err := wp.resolvePeople(ctx, st); err != nil {
		return nil, err
	}
	if !taste.usable {
		if wp.vectors, err = loadVectors(ctx, st, w.catalog); err != nil {
			return nil, err
		}
	}
	wp.planPeople(taste)
	steps := []func(context.Context, *store.Store) error{wp.planFriendships, wp.planRequests, wp.planPlans, wp.planPosts}
	for _, step := range steps {
		if err := step(ctx, st); err != nil {
			return nil, err
		}
	}
	return wp, nil
}

// findSandy is the demo account; why says what is wrong when it cannot be used.
func findSandy(ctx context.Context, st *store.Store, email string) (*models.User, string, error) {
	u, err := findUser(ctx, st, bson.M{"email": strings.ToLower(strings.TrimSpace(email))})
	switch {
	case err != nil:
		return nil, "", err
	case u == nil:
		return nil, fmt.Sprintf("no account %s (the demo account, Sandy Byte; --demo-email picks another)", email), nil
	case !store.IsDemoCast(&u.User):
		return nil, fmt.Sprintf("%s is not a demo account (roles %v), so it does not live in Saltlight", email, u.Roles), nil
	}
	return &u.User, "", nil
}

// findUser is the user matching filter, nil when there is none.
func findUser(ctx context.Context, st *store.Store, filter bson.M) (*storedUser, error) {
	var u storedUser
	err := st.Collection(store.CollUsers).FindOne(ctx, filter).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("look up users: %w", err)
	}
	return &u, nil
}

// list keeps $in and $nin arguments arrays: a nil slice would encode as null.
func list[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// stored is a document found under one of the seed's ids.
type stored struct {
	ours bool
	raw  bson.Raw
}

func idKey(id any) string {
	if oid, ok := id.(bson.ObjectID); ok {
		return oid.Hex()
	}
	return fmt.Sprint(id)
}

// rawKey is idKey of a stored _id.
func rawKey(v bson.RawValue) string {
	if oid, ok := v.ObjectIDOK(); ok {
		return oid.Hex()
	}
	if s, ok := v.StringValueOK(); ok {
		return s
	}
	return v.String()
}

// existing loads the documents of coll among ids, keyed by idKey; ours
// are the showcase seed's.
func existing(ctx context.Context, st *store.Store, coll string, ids []any) (map[string]stored, error) {
	return existingTagged(ctx, st, coll, ids, seedTag)
}

// existingTagged is existing for the documents tagged tag.
func existingTagged(ctx context.Context, st *store.Store, coll string, ids []any, tag string) (map[string]stored, error) {
	out := map[string]stored{}
	if len(ids) == 0 {
		return out, nil
	}
	cursor, err := st.Collection(coll).Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", coll, err)
	}
	var raws []bson.Raw
	if err := cursor.All(ctx, &raws); err != nil {
		return nil, fmt.Errorf("read %s: %w", coll, err)
	}
	for _, raw := range raws {
		have, _ := raw.Lookup(fieldSeed).StringValueOK()
		out[rawKey(raw.Lookup("_id"))] = stored{ours: have == tag, raw: raw}
	}
	return out, nil
}

// classify is create, update or conflict for a document under a fixed id.
func classify(have map[string]stored, id any) (action, string) {
	d, ok := have[idKey(id)]
	switch {
	case !ok:
		return actCreate, ""
	case d.ours:
		return actUpdate, ""
	}
	return actConflict, "a document that is not this seed's has its id"
}

func (wp *worldPlan) section(title string) *section {
	s := &section{title: title}
	wp.sections = append(wp.sections, s)
	return s
}

// known reports whether id is one of the world's people (the cast or Sandy).
func (wp *worldPlan) known(id string) bool {
	for _, v := range wp.ids {
		if v == id {
			return true
		}
	}
	return false
}

// ---- people ----------------------------------------------------------------

func (wp *worldPlan) resolvePeople(ctx context.Context, st *store.Store) error {
	ids := make([]any, 0, len(wp.spec.cast))
	for _, p := range wp.spec.cast {
		ids = append(ids, p.id)
	}
	have, err := existing(ctx, st, store.CollUsers, ids)
	if err != nil {
		return err
	}
	for _, p := range wp.spec.cast {
		m := &member{person: p, uid: p.id}
		switch d, ok := have[p.id.Hex()]; {
		case ok && d.ours:
			var u models.User
			if err := bson.Unmarshal(d.raw, &u); err != nil {
				return fmt.Errorf("read %s: %w", p.name, err)
			}
			m.act, m.current = actUpdate, &u
		case ok:
			m.act, m.note = actConflict, "its fixed id "+p.id.Hex()+" belongs to another account"
		default:
			if err := resolveNew(ctx, st, m); err != nil {
				return err
			}
		}
		wp.members = append(wp.members, m)
		if m.act != actConflict {
			wp.ids[p.key] = m.uid.Hex()
			wp.names[m.uid.Hex()] = p.name
			if m.current != nil {
				wp.names[m.uid.Hex()] = m.current.Name
			}
		}
	}
	return nil
}

// resolveNew decides about a person the seed has not written under its id:
// an account at one of their adopt addresses in the demo cast is used as
// is; anyone else holding their email or handle is a conflict.
func resolveNew(ctx context.Context, st *store.Store, m *member) error {
	for _, email := range m.adoptEmails {
		u, err := findUser(ctx, st, bson.M{"email": email})
		if err != nil {
			return err
		}
		switch {
		case u == nil:
			continue
		case u.Seed == seedTag:
			m.act, m.uid, m.current = actUpdate, u.ID, &u.User
		case store.IsDemoCast(&u.User):
			m.act, m.uid, m.current = actKeep, u.ID, &u.User
			m.note = "exists already, not made by this seed; used as it is, never changed"
		default:
			m.act, m.note = actConflict, email+" belongs to an account outside the demo cast"
		}
		return nil
	}
	if u, err := findUser(ctx, st, bson.M{"email": m.email}); err != nil {
		return err
	} else if u != nil {
		m.act, m.note = actConflict, m.email+" belongs to another account"
		return nil
	}
	if u, err := findUser(ctx, st, bson.M{"usernameLower": strings.ToLower(m.username)}); err != nil {
		return err
	} else if u != nil {
		m.act, m.note = actConflict, "@"+m.username+" belongs to another account, "+u.ID.Hex()
		return nil
	}
	m.act = actCreate
	return nil
}

// planPeople lists the cast and decides how the taste vectors of each
// person the seed writes are made.
func (wp *worldPlan) planPeople(taste tasteSource) {
	s := wp.section("People")
	for i, m := range wp.members {
		label := fmt.Sprintf("%s · @%s", m.name, m.username)
		if m.current != nil && m.act == actKeep {
			label = fmt.Sprintf("%s · @%s", m.current.Name, m.current.Username)
		}
		if m.school != "" && m.act != actKeep {
			label += " · " + m.school
		}
		if m.act == actCreate || m.act == actUpdate {
			label += " · " + strings.ReplaceAll(string(m.status), "_", " ")
			switch {
			case taste.usable:
				m.taste = "taste vectors from the ML service"
			case m.current != nil && m.current.ProfileTextHash != "":
				m.taste = "taste vectors kept (built by the ML service earlier)"
			default:
				var n int
				m.pos, n = blend(m.prefs.Ratings, wp.vectors, true)
				m.neg, _ = blend(m.prefs.Ratings, wp.vectors, false)
				m.taste = fmt.Sprintf("taste vectors blended from %d catalog items", n)
				if m.pos == nil {
					m.taste = "no taste vectors (the catalog has none for what they like)"
				}
			}
			label += " · " + m.taste
		}
		s.changes = append(s.changes, change{coll: store.CollUsers, id: m.uid, act: m.act, label: label, note: m.note,
			doc: wp.userDoc(m, i)})
	}
}

// userDoc is the person's account as the API reads it (users, pkg/models).
// Account metadata is wall-clock time; they were active in the last hour.
func (wp *worldPlan) userDoc(m *member, i int) *models.User {
	birth := m.birth
	u := &models.User{
		ID: m.uid, Email: m.email, Name: m.name, NameLower: strings.ToLower(m.name),
		Username: m.username, UsernameLower: strings.ToLower(m.username),
		AvatarColor: string(m.avatar), Status: string(m.status), BirthDate: &birth, SetupComplete: true, City: wp.spec.city,
		Prefs:        prefsDoc(m.prefs),
		Taste:        models.UserTaste{Tags: map[string]float64{}},
		Integrations: models.UserIntegrations{},
		Roles:        wp.spec.roles,
		CreatedAt:    wp.clock.real.UTC(), UpdatedAt: wp.clock.real.UTC(),
		LastActiveAt: wp.clock.real.Add(-time.Duration(3+7*i) * time.Minute).UTC(),
	}
	if m.school != "" {
		school := m.school
		u.School = &school
	}
	return u
}

// prefsDoc stores validated preferences the way PUT /me/preferences does.
func prefsDoc(p contract.Preferences) models.UserPrefs {
	ratings := make(map[string]int, len(p.Ratings))
	maps.Copy(ratings, p.Ratings)
	answers := map[string]string{}
	for _, key := range contract.AnswerKeys {
		if text := strings.TrimSpace(p.Answers[key]); text != "" {
			answers[key] = text
		}
	}
	return models.UserPrefs{
		Ratings: ratings, Company: string(p.Company), Pace: string(p.Pace), Spend: string(p.Spend),
		Flexibility: string(p.Flexibility), SplitStyle: string(p.SplitStyle), PreferFree: p.PreferFree, Answers: answers,
		InstantCheckout: p.InstantCheckout, InstantCheckoutLimitCents: p.InstantCheckoutLimitCents,
	}
}

// ---- friends -----------------------------------------------------------------

// pendingBetween are the pending requests (not the seed's) between any two
// of ids, as friendship ids.
func pendingBetween(ctx context.Context, st *store.Store, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	cursor, err := st.Collection(store.CollFriendRequests).Find(ctx, bson.M{
		"status": models.RequestPending, fieldSeed: bson.M{"$ne": seedTag},
		"fromId": bson.M{"$in": list(ids)}, "toId": bson.M{"$in": list(ids)},
	})
	if err != nil {
		return nil, fmt.Errorf("read friend requests: %w", err)
	}
	var reqs []models.FriendRequest
	if err := cursor.All(ctx, &reqs); err != nil {
		return nil, fmt.Errorf("read friend requests: %w", err)
	}
	for _, r := range reqs {
		out[models.FriendshipID(r.FromID, r.ToID)] = true
	}
	return out, nil
}

func (wp *worldPlan) allIDs() []string {
	return slices.Sorted(maps.Values(wp.ids))
}

// sortedIDs is a friendship's user ids, sorted.
func sortedIDs(a, b string) []string {
	ids := []string{a, b}
	slices.Sort(ids)
	return ids
}

func (wp *worldPlan) planFriendships(ctx context.Context, st *store.Store) error {
	s := wp.section("Friendships")
	var ids []any
	for _, p := range wp.spec.friendships {
		if a, b := wp.ids[p.a], wp.ids[p.b]; a != "" && b != "" {
			ids = append(ids, models.FriendshipID(a, b))
		}
	}
	have, err := existing(ctx, st, store.CollFriendships, ids)
	if err != nil {
		return err
	}
	pending, err := pendingBetween(ctx, st, wp.allIDs())
	if err != nil {
		return err
	}
	for _, p := range wp.spec.friendships {
		a, b := wp.ids[p.a], wp.ids[p.b]
		if a == "" || b == "" {
			continue
		}
		id := models.FriendshipID(a, b)
		c := change{coll: store.CollFriendships, id: id, label: wp.names[a] + " – " + wp.names[b],
			doc: &models.Friendship{ID: id, UserIDs: sortedIDs(a, b), CreatedAt: wp.clock.now.Add(-time.Duration(p.days) * 24 * time.Hour).UTC()}}
		switch d, ok := have[id]; {
		case ok && d.ours:
			c.act = actUpdate
		case ok:
			c.act, c.note = actKeep, "already friends"
		case pending[id]:
			c.act, c.note = actKeep, "a friend request between them is waiting; left as it is"
		default:
			c.act = actCreate
		}
		s.changes = append(s.changes, c)
	}
	return nil
}

func (wp *worldPlan) planRequests(ctx context.Context, st *store.Store) error {
	if len(wp.spec.requests) == 0 {
		return nil
	}
	s := wp.section("Friend requests")
	var ids, pairs []any
	for _, r := range wp.spec.requests {
		ids = append(ids, wp.requestID(r))
		if a, b := wp.ids[r.from], wp.ids[r.to]; a != "" && b != "" {
			pairs = append(pairs, models.FriendshipID(a, b))
		}
	}
	have, err := existing(ctx, st, store.CollFriendRequests, ids)
	if err != nil {
		return err
	}
	friends, err := existing(ctx, st, store.CollFriendships, pairs)
	if err != nil {
		return err
	}
	pending, err := pendingBetween(ctx, st, wp.allIDs())
	if err != nil {
		return err
	}
	for _, r := range wp.spec.requests {
		from, to := wp.ids[r.from], wp.ids[r.to]
		if from == "" || to == "" {
			continue
		}
		id := wp.requestID(r)
		at := wp.clock.now.Add(-r.ago).UTC()
		c := change{coll: store.CollFriendRequests, id: id, label: fmt.Sprintf("%s → %s · “%s”", wp.names[from], wp.names[to], r.note),
			doc: &models.FriendRequest{ID: id, FromID: from, ToID: to, Note: r.note, Status: models.RequestPending, CreatedAt: at, UpdatedAt: at}}
		_, areFriends := friends[models.FriendshipID(from, to)]
		act, note := classify(have, id)
		switch {
		case act == actUpdate && areFriends:
			act, note = actKeep, "accepted: they are friends now; left as it is"
		case act == actCreate && areFriends:
			act, note = actKeep, "they are friends already"
		case act == actCreate && pending[models.FriendshipID(from, to)]:
			act, note = actKeep, "a request between them is waiting already"
		}
		c.act, c.note = act, note
		s.changes = append(s.changes, c)
	}
	return nil
}

func (wp *worldPlan) requestID(r requestSpec) string {
	return fmt.Sprintf("showcase-%s-request-%s-%s", wp.short, r.from, r.to)
}

// ---- plans, their chats --------------------------------------------------------

func (wp *worldPlan) planPlans(ctx context.Context, st *store.Store) error {
	s := wp.section("Plans")
	var planIDs, threadIDs, msgIDs []any
	for _, p := range wp.spec.plans {
		planIDs = append(planIDs, wp.planID(p.key))
		threadIDs = append(threadIDs, wp.threadID(p.key))
		for i := range p.chat {
			msgIDs = append(msgIDs, wp.messageID(p.key, i))
		}
	}
	havePlans, err := existing(ctx, st, store.CollItineraries, planIDs)
	if err != nil {
		return err
	}
	haveThreads, err := existing(ctx, st, store.CollThreads, threadIDs)
	if err != nil {
		return err
	}
	haveMsgs, err := existing(ctx, st, store.CollMessages, msgIDs)
	if err != nil {
		return err
	}
	// A group chat is unique per itinerary: another thread on a seeded plan is in the way.
	cursor, err := st.Collection(store.CollThreads).Find(ctx, bson.M{"itineraryId": bson.M{"$in": list(planIDs)}, "_id": bson.M{"$nin": list(threadIDs)}},
		options.Find().SetProjection(bson.M{"itineraryId": 1}))
	if err != nil {
		return fmt.Errorf("read threads: %w", err)
	}
	var others []models.Thread
	if err := cursor.All(ctx, &others); err != nil {
		return fmt.Errorf("read threads: %w", err)
	}
	otherChat := map[string]bool{}
	for _, th := range others {
		otherChat[th.ItineraryID] = true
	}

	used := map[string]bool{}
	messages, chats := 0, 0
	for i := range wp.spec.plans {
		spec := &wp.spec.plans[i]
		host := wp.ids[spec.host]
		if host == "" {
			wp.notes = append(wp.notes, fmt.Sprintf("plan “%s” left out: its host is not available", spec.title))
			continue
		}
		lay, ok := wp.catalog.layOut(wp.spec, spec, wp.clock.now, wp.clock.loc, used)
		if !ok {
			wp.notes = append(wp.notes, fmt.Sprintf("plan “%s” left out: no place in %s is open for its first stop near where it meets",
				spec.title, wp.spec.catalog))
			continue
		}
		planID, threadID := wp.planID(spec.key), wp.threadID(spec.key)
		plan, thread, msgs, err := wp.planDocs(spec, lay, host, havePlans[planID], haveThreads[threadID])
		if err != nil {
			return err
		}
		c := change{coll: store.CollItineraries, id: planID, doc: plan, label: wp.planLabel(plan),
			detail: routeLine(lay) + fmt.Sprintf(" · group chat, %d messages", len(msgs))}
		c.act, c.note = classify(havePlans, planID)
		threadAct, threadNote := classify(haveThreads, threadID)
		if threadAct == actCreate && otherChat[planID] {
			threadAct, threadNote = actConflict, "another group chat already belongs to this plan"
		}
		if c.act != actConflict && threadAct == actConflict {
			c.act, c.note = actConflict, "its group chat: "+threadNote
		}
		s.changes = append(s.changes, c, change{coll: store.CollThreads, id: threadID, act: threadAct, note: threadNote, doc: thread})
		for _, m := range msgs {
			act, note := classify(haveMsgs, m.ID)
			s.changes = append(s.changes, change{coll: store.CollMessages, id: m.ID, act: act, note: note, doc: m})
			messages++
		}
		chats++
		wp.threads = append(wp.threads, threadID)
	}
	s.extra = fmt.Sprintf("with %d group chats, %d messages", chats, messages)
	return nil
}

func (wp *worldPlan) planID(key string) string {
	return fmt.Sprintf("showcase-%s-plan-%s", wp.short, key)
}
func (wp *worldPlan) threadID(key string) string {
	return fmt.Sprintf("showcase-%s-chat-%s", wp.short, key)
}
func (wp *worldPlan) messageID(key string, i int) string {
	return fmt.Sprintf("showcase-%s-msg-%s-%d", wp.short, key, i+1)
}

// planDocs are a plan's itinerary, group chat and messages. People who
// joined the seeded plan or chat themselves stay in them.
func (wp *worldPlan) planDocs(spec *planSpec, lay laidOut, host string, prevPlan, prevChat stored) (
	*models.Itinerary, *models.Thread, []*models.Message, error) {
	now, loc := wp.clock.now, wp.clock.loc
	planID, threadID := wp.planID(spec.key), wp.threadID(spec.key)
	memberIDs := []string{host}
	for _, key := range spec.members {
		if id := wp.ids[key]; id != "" && !slices.Contains(memberIDs, id) {
			memberIDs = append(memberIDs, id)
		}
	}
	seeded := slices.Clone(memberIDs)
	if prevPlan.ours {
		var old models.Itinerary
		if err := bson.Unmarshal(prevPlan.raw, &old); err != nil {
			return nil, nil, nil, fmt.Errorf("read %s: %w", planID, err)
		}
		for _, id := range old.MemberIDs {
			if !wp.known(id) && !slices.Contains(memberIDs, id) {
				memberIDs = append(memberIDs, id)
			}
		}
	}

	title := spec.title
	if !lay.exact {
		title = "Evening at " + lay.stops[0].place.Name
		if lay.start.In(loc).Hour() < 17 {
			title = "Afternoon at " + lay.stops[0].place.Name
		}
	}
	created := now.Add(-spec.createdAgo).UTC()
	startLocal := lay.start.In(loc)
	date := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	plan := &models.Itinerary{
		ID: planID, HostID: host, MemberIDs: memberIDs, Title: title,
		DateKey: date.Format("2006-01-02"), TZ: loc.String(), Date: date.UTC(),
		Start: lay.start.UTC(), BackBy: lay.backBy.UTC(),
		StartPlace: placeDoc(lay.stops[0].place), EndPlace: placeDoc(lay.stops[len(lay.stops)-1].place),
		Visibility: spec.visibility, Items: items(planID, lay.stops),
		Plan: &models.PlanSnapshot{Range: string(contract.RangeWalkable), Ride: string(contract.RideNone), MoodText: spec.mood,
			Tags: planTags(lay.stops), Budget: planBudget(lay.stops), Who: spec.visibility, Pace: string(contract.PaceBalanced),
			Modes: []string{string(contract.ModeWalk)}},
		RouteMode: string(contract.ModeWalk), Status: models.ItineraryActive, ThreadID: threadID,
		CreatedAt: created, UpdatedAt: created,
	}
	if spec.maxSize > 0 {
		size := spec.maxSize
		plan.MaxGroupSize = &size
	}
	if lock := lay.start.Add(-30 * time.Minute); spec.visibility == models.VisibilityOpen && lock.After(now.Add(15*time.Minute)) {
		lockAt := lock.UTC()
		plan.LockAt = &lockAt
	}

	var msgs []*models.Message
	for i, line := range spec.chat {
		sender := wp.ids[line.from]
		if sender == "" {
			continue
		}
		sent := now.Add(-line.ago).UTC()
		msgs = append(msgs, &models.Message{ID: wp.messageID(spec.key, i), ThreadID: threadID, SenderID: sender,
			Text: chatText(line.text, lay, sent, loc), SentAt: sent})
	}
	thread := &models.Thread{ID: threadID, IsGroup: true, Title: title, MemberIDs: slices.Clone(memberIDs), ItineraryID: planID,
		CreatedBy: host, LastMessageAt: created, Unread: map[string]int{}, ReadAt: map[string]time.Time{},
		CreatedAt: created, UpdatedAt: created}
	if n := len(msgs); n > 0 {
		last := msgs[n-1]
		thread.LastMessageText, thread.LastSenderID, thread.LastMessageAt, thread.UpdatedAt = last.Text, last.SenderID, last.SentAt, last.SentAt
	}
	for _, id := range seeded {
		unread := 0
		for key, count := range spec.unread {
			if wp.ids[key] == id {
				unread = min(count, len(msgs))
			}
		}
		thread.Unread[id] = unread
		thread.ReadAt[id] = thread.LastMessageAt
		if unread > 0 {
			thread.ReadAt[id] = created
			if k := len(msgs) - unread - 1; k >= 0 {
				thread.ReadAt[id] = msgs[k].SentAt
			}
		}
	}
	if prevChat.ours {
		var old models.Thread
		if err := bson.Unmarshal(prevChat.raw, &old); err != nil {
			return nil, nil, nil, fmt.Errorf("read %s: %w", threadID, err)
		}
		for _, id := range old.MemberIDs {
			if !wp.known(id) && !slices.Contains(thread.MemberIDs, id) {
				thread.MemberIDs = append(thread.MemberIDs, id)
			}
		}
		for _, id := range thread.MemberIDs {
			if slices.Contains(seeded, id) {
				continue
			}
			if n, ok := old.Unread[id]; ok {
				thread.Unread[id] = n
			}
			if t, ok := old.ReadAt[id]; ok {
				thread.ReadAt[id] = t
			}
		}
	}
	return plan, thread, msgs, nil
}

// chatText fills a chat line's {s1}…{s3}, {start} and {when} (as seen when
// the message was sent).
func chatText(text string, lay laidOut, sent time.Time, loc *time.Location) string {
	stop := func(i int) string { return lay.stops[min(i, len(lay.stops)-1)].place.Name }
	start := lay.start.In(loc)
	return strings.NewReplacer("{s1}", stop(0), "{s2}", stop(1), "{s3}", stop(2),
		"{start}", httpx.ClockShort(start), "{when}", whenWord(start, sent.In(loc))).Replace(text)
}

// whenWord is how a day reads from another: "tonight", "today", "tomorrow"
// or "on Monday".
func whenWord(t, from time.Time) string {
	switch httpx.DayDiff(t, from) {
	case 0:
		if t.Hour() >= 17 {
			return "tonight"
		}
		return "today"
	case 1:
		return "tomorrow"
	}
	return "on " + t.Format("Monday")
}

func (wp *worldPlan) planLabel(it *models.Itinerary) string {
	loc := wp.clock.loc
	start, end := it.Start.In(loc), it.BackBy.In(loc)
	who := it.Visibility
	if it.Visibility == models.VisibilityFriends {
		who = "friends only"
	}
	size := fmt.Sprintf("%d going", len(it.MemberIDs))
	if it.MaxGroupSize != nil {
		size += fmt.Sprintf(", max %d", *it.MaxGroupSize)
	}
	return fmt.Sprintf("%s · %s, %s · %s (%s) · host %s", it.Title, start.Format("Mon Jan 2"), httpx.TimeRange(start, end),
		who, size, wp.names[it.HostID])
}

func routeLine(lay laidOut) string {
	names := make([]string, len(lay.stops))
	for i, s := range lay.stops {
		names[i] = s.place.Name
	}
	return strings.Join(names, " → ")
}

// ---- free-now posts --------------------------------------------------------------

func (wp *worldPlan) planPosts(ctx context.Context, st *store.Store) error {
	s := wp.section("Free-now posts")
	var ids []any
	var authors []string
	for _, p := range wp.spec.posts {
		ids = append(ids, wp.postID(p.author))
		if id := wp.ids[p.author]; id != "" {
			authors = append(authors, id)
		}
	}
	have, err := existing(ctx, st, store.CollForumPosts, ids)
	if err != nil {
		return err
	}
	// One free-now post per author (a unique index): another one is in the way.
	cursor, err := st.Collection(store.CollForumPosts).Find(ctx, bson.M{"type": models.PostFreeNow,
		"authorId": bson.M{"$in": list(authors)}, "_id": bson.M{"$nin": list(ids)}})
	if err != nil {
		return fmt.Errorf("read forum posts: %w", err)
	}
	var others []models.ForumPost
	if err := cursor.All(ctx, &others); err != nil {
		return fmt.Errorf("read forum posts: %w", err)
	}
	busy := map[string]bool{}
	for _, p := range others {
		busy[p.AuthorID] = true
	}
	now, loc := wp.clock.now, wp.clock.loc
	for _, p := range wp.spec.posts {
		author := wp.ids[p.author]
		if author == "" {
			continue
		}
		pt := p.point
		if a := wp.catalog.byName[strings.ToLower(p.place)]; p.place != "" && a != nil {
			pt = point(a)
		}
		until := freeUntil(p.untilHour, now, loc)
		area := p.area
		post := &models.ForumPost{
			ID: wp.postID(p.author), Type: models.PostFreeNow, AuthorID: author, Visibility: p.visibility,
			Location:  models.GeoJSONPoint{Type: "Point", Coordinates: []float64{pt.Lng, pt.Lat}},
			AreaLabel: &area, Text: "Free until " + httpx.Clock(until.In(loc)) + " near " + area,
			Until: until.UTC(), ExpiresAt: wp.clock.expiry(until).UTC(), CreatedAt: now.Add(-p.ago).UTC(),
		}
		c := change{coll: store.CollForumPosts, id: post.ID, doc: post,
			label: fmt.Sprintf("%s · “%s” · %s", wp.names[author], post.Text, p.visibility)}
		c.act, c.note = classify(have, post.ID)
		if c.act == actCreate && busy[author] {
			c.act, c.note = actConflict, "they have another free-now post"
		}
		s.changes = append(s.changes, c)
	}
	return nil
}

func (wp *worldPlan) postID(author string) string {
	return fmt.Sprintf("showcase-%s-free-%s", wp.short, author)
}

// freeUntil is tonight at hour, or two hours from now (on the half hour)
// when that is less than an hour away.
func freeUntil(hour int, now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	until := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, loc)
	if until.Before(now.Add(time.Hour)) {
		until = now.Add(2 * time.Hour).In(loc)
		until = time.Date(until.Year(), until.Month(), until.Day(), until.Hour(), until.Minute()/30*30, 0, 0, loc).Add(30 * time.Minute)
	}
	return until
}
