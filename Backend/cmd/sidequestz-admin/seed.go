package main

// seed-demo (backend-contract §7, docs/DEMO.md): Sandy Byte, her two bot
// friends and the world the three-minute walkthrough shows, in the demo city
// (catalog demo_activities, city saltlight). Every document has a fixed id
// ("seed-…"; users "5eed…" ObjectIDs), so running it again rewrites the same
// documents instead of adding new ones, and restores the seeded state of
// what a walkthrough changes (the unrated stop, Theo's pending request).
// Times hang off the run in DEMO_TZ, so "tomorrow" stays tomorrow.

import (
	"Backend/pkg/api"
	"Backend/pkg/config"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"Backend/pkg/util"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// seedProfileTimeout bounds the ML refresh of Sandy's taste vectors.
const seedProfileTimeout = 20 * time.Second

const (
	demoCatalog = store.CollDemoActivities
	demoCity    = "saltlight"
	walkNote    = "Route options below. Times update live if you run late."
)

// Fixed ids of everything the seed writes.
const (
	seedOpenPlanID   = "seed-itin-open-plan"
	seedCrewPlanID   = "seed-itin-market-crew"
	seedSoloPlanID   = "seed-itin-solo-walk"
	seedCrewThreadID = "seed-thread-market-crew"
	seedDMThreadID   = "seed-thread-dm-sandy-marin"
	seedFreePostID   = "seed-post-marin-free"
	seedRequestID    = "seed-fr-theo-sandy"
	seedCardID       = "seed-pm-sandy-visa"
)

// seedPerson is one seeded account.
type seedPerson struct {
	id       bson.ObjectID // used unless an account with the email exists
	email    string
	name     string
	username string
	avatar   string
	role     string
}

var (
	sandy = seedPerson{id: mustObjectID("5eed00000000000000000001"), email: "demo@sidequestz.tech", name: "Sandy Byte",
		username: "sandybyte", avatar: "sage", role: "demo"}
	marin = seedPerson{id: mustObjectID("5eed00000000000000000002"), email: "marin@bots.sidequestz.tech", name: "Marin Okafor",
		username: "marinokafor", avatar: "clay", role: "bot"}
	theo = seedPerson{id: mustObjectID("5eed00000000000000000003"), email: "theo@bots.sidequestz.tech", name: "Theo Park",
		username: "theopark", avatar: "forest", role: "bot"}
)

// sandyHomeBase is where every Saltlight plan starts.
var sandyHomeBase = models.HomeBase{Name: "Seaside Market Square", Lat: 31.3680, Lng: -81.4250}

// sandyPrefs is her setup: what she enjoys, how she plans and her answers.
var sandyPrefs = models.UserPrefs{
	Ratings: map[string]int{
		"outdoors": 5, "long_walks": 5, "live_music": 4, "food": 4, "early_mornings": 4,
		"museums": 3, "sports": 3, "shopping": 2, "nightlife": 2, "big_crowds": 1,
	},
	Company: "small_group", Pace: "balanced", Spend: "under_15", Flexibility: "bit_over_ok", SplitStyle: "equally",
	PreferFree: true,
	Answers: map[string]string{
		"perfect_afternoon": "A long walk along the water, a snack from the market, then live music somewhere small while the sun goes down.",
		"never_do":          "Packed clubs, huge crowds, or anything that only gets going after midnight.",
		"plan_around":       "Sunrise swims, the Saturday market, and whoever's free to wander.",
	},
	InstantCheckout:           false,
	InstantCheckoutLimitCents: 5000,
}

func mustObjectID(hex string) bson.ObjectID {
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		panic("seed: " + err.Error())
	}
	return id
}

// runSeedDemo is `sidequestz-admin seed-demo`. profiles refreshes Sandy's
// taste vectors through the ML service; nil leaves them pending.
func runSeedDemo(ctx context.Context, st *store.Store, cfg *config.Config, profiles api.Profiles) int {
	report, err := seedDemo(ctx, st, cfg, profiles, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed-demo: %v\n", err)
		return 1
	}
	report.print(os.Stdout, cfg.MongoDB)
	return 0
}

// seedReport is what a run wrote, for the operator.
type seedReport struct {
	SandyID  string
	Counts   []seedCount
	OpenPlan string
	Vectors  string
}

type seedCount struct {
	Collection string
	N          int
}

func (r *seedReport) print(w io.Writer, db string) {
	parts := make([]string, 0, len(r.Counts))
	for _, c := range r.Counts {
		parts = append(parts, fmt.Sprintf("%s %d", c.Collection, c.N))
	}
	fmt.Fprintf(w, "seeded the demo in %s: Sandy Byte (%s, id %s), city %s, catalog %s\n", db, sandy.email, r.SandyID, demoCity, demoCatalog)
	fmt.Fprintf(w, "  documents: %s\n", strings.Join(parts, ", "))
	fmt.Fprintf(w, "  Marin's open plan: %s\n", r.OpenPlan)
	fmt.Fprintf(w, "  Sandy's taste vectors: %s\n", r.Vectors)
}

// seedDemo creates or refreshes the demo world as of now. It needs
// DEMO_PASSWORD (Sandy's password) and the Saltlight catalog; the whole
// world is built in memory first, so a missing catalog writes nothing.
func seedDemo(ctx context.Context, st *store.Store, cfg *config.Config, profiles api.Profiles, now time.Time) (*seedReport, error) {
	if cfg.DemoPassword == "" {
		return nil, errors.New("DEMO_PASSWORD is required (Sandy Byte's password)")
	}
	tzName := cfg.DemoTZ
	if tzName == "" {
		tzName = httpx.DefaultTimeZone
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return nil, fmt.Errorf("DEMO_TZ %q: %w", tzName, err)
	}
	now = now.UTC().Truncate(time.Millisecond)

	places, err := loadSeedPlaces(ctx, st)
	if err != nil {
		return nil, err
	}
	accounts := map[string]*seedAccount{}
	for _, p := range []seedPerson{sandy, marin, theo} {
		acct, err := resolveSeedAccount(ctx, st, p)
		if err != nil {
			return nil, err
		}
		accounts[p.username] = acct
	}
	ids := seedIDs{sandy: accounts[sandy.username].id.Hex(), marin: accounts[marin.username].id.Hex(), theo: accounts[theo.username].id.Hex()}
	world, err := buildSeedWorld(ids, places, now, loc)
	if err != nil {
		return nil, err
	}
	// A DM between Sandy and Marin may already exist (Plan together on
	// Marin's post); the seeded messages go into that thread then.
	if err := adoptExistingDM(ctx, st, world); err != nil {
		return nil, err
	}

	for _, p := range []seedPerson{sandy, marin, theo} {
		if err := writeSeedAccount(ctx, st, p, accounts[p.username], cfg.DemoPassword, now); err != nil {
			return nil, err
		}
	}
	counts, err := writeSeedWorld(ctx, st, world)
	if err != nil {
		return nil, err
	}
	report := &seedReport{SandyID: ids.sandy, Counts: counts, OpenPlan: world.openPlanSummary}

	// Last: the refresh reads the preferences and rated stops written above.
	report.Vectors = "pending (no ML profile refresher is wired into sidequestz-admin; the ML wiring or Sandy's next PUT /me/preferences computes them)"
	if profiles != nil {
		rctx, cancel := context.WithTimeout(ctx, seedProfileTimeout)
		err := profiles.Refresh(rctx, ids.sandy)
		cancel()
		if err != nil {
			report.Vectors = fmt.Sprintf("not refreshed (%v)", err)
		} else {
			report.Vectors = "refreshed through the ML service"
		}
	}
	stored := "none"
	if user, err := st.Users().ByID(ctx, ids.sandy); err == nil && len(user.PositiveEmbedding) > 0 {
		stored = fmt.Sprintf("%d-d, model %q", len(user.PositiveEmbedding), user.EmbeddingModel)
	}
	report.Vectors += "; stored: " + stored
	return report, nil
}

// ---- accounts ----------------------------------------------------------------

// seedAccount is a seeded person's id and, when it exists, their document.
type seedAccount struct {
	id       bson.ObjectID
	existing *models.User
}

// resolveSeedAccount finds the account behind p: the one holding p's email
// (so an account made by hand is adopted, not duplicated), else p's fixed id.
// Someone else holding p's username stops the seed before it writes anything.
func resolveSeedAccount(ctx context.Context, st *store.Store, p seedPerson) (*seedAccount, error) {
	acct, err := findSeedAccount(ctx, st, p)
	if err != nil {
		return nil, err
	}
	holder, err := st.Users().ByUsername(ctx, p.username)
	switch {
	case err == nil && holder.ID != acct.id:
		return nil, fmt.Errorf("%s: @%s belongs to another account (%s, %s); rename or delete it first",
			p.name, p.username, holder.ID.Hex(), holder.Email)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return nil, fmt.Errorf("look up @%s: %w", p.username, err)
	}
	return acct, nil
}

func findSeedAccount(ctx context.Context, st *store.Store, p seedPerson) (*seedAccount, error) {
	user, err := st.Users().ByEmail(ctx, p.email)
	if err == nil {
		return &seedAccount{id: user.ID, existing: user}, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("look up %s: %w", p.email, err)
	}
	user, err = st.Users().ByID(ctx, p.id.Hex())
	switch {
	case err == nil:
		return &seedAccount{id: p.id, existing: user}, nil
	case errors.Is(err, store.ErrNotFound):
		return &seedAccount{id: p.id}, nil
	}
	return nil, fmt.Errorf("look up %s: %w", p.name, err)
}

// writeSeedAccount upserts one account. Sandy's profile and preferences are
// reset to the demo's; her password is set to DEMO_PASSWORD (kept when it
// already matches); the bots get a random password once. Vectors and other
// fields the seed does not own are left alone, except that changed
// preferences mark Sandy's vectors stale (profileTextHash cleared).
func writeSeedAccount(ctx context.Context, st *store.Store, p seedPerson, acct *seedAccount, demoPassword string, now time.Time) error {
	set := bson.M{
		"email": p.email, "name": p.name, "nameLower": strings.ToLower(p.name),
		"username": p.username, "usernameLower": strings.ToLower(p.username),
		"avatarColor": p.avatar, "status": "open", "setupComplete": true,
		"catalog": demoCatalog, "city": demoCity, "roles": []string{p.role}, "updatedAt": now,
	}
	insert := bson.M{"createdAt": now, "lastActiveAt": now, "integrations": models.UserIntegrations{}}
	update := bson.M{"$set": set, "$setOnInsert": insert}
	if p.username == sandy.username {
		birth := time.Date(2003, time.June, 14, 0, 0, 0, 0, time.UTC)
		set["birthDate"] = birth
		set["school"] = "Saltlight Harbor College"
		set["homeBase"] = sandyHomeBase
		set["prefs"] = sandyPrefs
		set["taste"] = models.UserTaste{Tags: map[string]float64{}}
		hash := ""
		if acct.existing != nil {
			if ok, err := util.VerifyPassword(demoPassword, acct.existing.PasswordHash); err == nil && ok {
				hash = acct.existing.PasswordHash
			}
			if !reflect.DeepEqual(acct.existing.Prefs, sandyPrefs) {
				update["$unset"] = bson.M{"profileTextHash": ""}
			}
		}
		if hash == "" {
			var err error
			if hash, err = util.HashPassword(demoPassword); err != nil {
				return err
			}
		}
		set["passwordHash"] = hash
	} else {
		insert["prefs"] = models.UserPrefs{Ratings: map[string]int{}, Answers: map[string]string{}}
		insert["taste"] = models.UserTaste{Tags: map[string]float64{}}
		if acct.existing == nil || acct.existing.PasswordHash == "" {
			hash, err := util.HashPassword(util.RandomToken(24))
			if err != nil {
				return err
			}
			set["passwordHash"] = hash
		}
	}
	_, err := st.Collection(store.CollUsers).UpdateOne(ctx, bson.M{"_id": acct.id}, update, options.UpdateOne().SetUpsert(true))
	if store.IsDuplicate(err) {
		return fmt.Errorf("%s (@%s, %s): another account already uses that email or username; rename or delete it first: %w",
			p.name, p.username, p.email, err)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", p.name, err)
	}
	return nil
}

// ---- catalog -----------------------------------------------------------------

// loadSeedPlaces reads the Saltlight places of the demo catalog (without
// their vectors).
func loadSeedPlaces(ctx context.Context, st *store.Store) ([]*models.Activity, error) {
	cursor, err := st.Collection(demoCatalog).Find(ctx, bson.M{"kind": "place", "city": demoCity},
		options.Find().SetProjection(bson.M{"embedding": 0, "embeddingText": 0}).SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", demoCatalog, err)
	}
	places := []*models.Activity{}
	if err := cursor.All(ctx, &places); err != nil {
		return nil, fmt.Errorf("read %s: %w", demoCatalog, err)
	}
	if len(places) < 3 {
		return nil, fmt.Errorf("%s has %d %s places; import dataingestion/demo/saltlight_harbor.json first", demoCatalog, len(places), demoCity)
	}
	return places, nil
}

func placePoint(a *models.Activity) travel.Point {
	if len(a.Location.Coordinates) < 2 {
		return travel.Point{}
	}
	return travel.Point{Lat: a.Location.Coordinates[1], Lng: a.Location.Coordinates[0]}
}

var homePoint = travel.Point{Lat: sandyHomeBase.Lat, Lng: sandyHomeBase.Lng}

// nearestPlaces are the n places closest to p (ties by id, so runs agree).
func nearestPlaces(places []*models.Activity, p travel.Point, n int) []*models.Activity {
	sorted := append([]*models.Activity(nil), places...)
	sort.SliceStable(sorted, func(i, j int) bool {
		di, dj := travel.HaversineKm(p, placePoint(sorted[i])), travel.HaversineKm(p, placePoint(sorted[j]))
		if di != dj {
			return di < dj
		}
		return sorted[i].ID.Hex() < sorted[j].ID.Hex()
	})
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

// findPlace is the place called name, else the one of category nearest the
// home base.
func findPlace(places []*models.Activity, name, category string) (*models.Activity, error) {
	for _, a := range places {
		if a.Name == name {
			return a, nil
		}
	}
	var same []*models.Activity
	for _, a := range places {
		if a.Category == category {
			same = append(same, a)
		}
	}
	if len(same) == 0 {
		return nil, fmt.Errorf("%s has neither %q nor any %s place; import dataingestion/demo/saltlight_harbor.json", demoCatalog, name, category)
	}
	return nearestPlaces(same, homePoint, 1)[0], nil
}

// ---- the world -----------------------------------------------------------------

type seedIDs struct{ sandy, marin, theo string }

// seedWorld is every non-user document of the demo.
type seedWorld struct {
	itineraries []models.Itinerary
	threads     []models.Thread
	messages    []models.Message
	expenses    []models.Expense
	ratings     []models.Rating
	friendships []models.Friendship
	requests    []models.FriendRequest
	posts       []models.ForumPost
	cards       []models.PaymentMethod

	sandyID, theoID string
	soloStopID      string // left unrated
	openPlanSummary string
}

// seedDays are the local days the demo hangs off.
type seedDays struct {
	today, tomorrow, yesterday, lastSaturday time.Time // local midnights
}

// demoDays: tomorrow carries Marin's open plan, yesterday the solo walk and
// the last Saturday before today (a week back on a Saturday) the market crew.
func demoDays(now time.Time, loc *time.Location) seedDays {
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	back := (int(today.Weekday()) - int(time.Saturday) + 7) % 7
	if back == 0 {
		back = 7
	}
	return seedDays{
		today:        today,
		tomorrow:     today.AddDate(0, 0, 1),
		yesterday:    today.AddDate(0, 0, -1),
		lastSaturday: today.AddDate(0, 0, -back),
	}
}

// at is a local wall-clock time on day.
func at(day time.Time, hour, minute int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
}

// buildSeedWorld lays out the demo as of now in loc.
func buildSeedWorld(ids seedIDs, places []*models.Activity, now time.Time, loc *time.Location) (*seedWorld, error) {
	days := demoDays(now, loc)
	crewMembers := []string{ids.sandy, ids.marin, ids.theo}
	w := &seedWorld{sandyID: ids.sandy, theoID: ids.theo}

	// Marin's open plan: tomorrow 17:30–20:00, the three places nearest the
	// market, walked nearest-first; joining locks at 17:00, six at most.
	start, backBy := at(days.tomorrow, 17, 30), at(days.tomorrow, 20, 0)
	route := walkOrder(homePoint, nearestPlaces(places, homePoint, 3))
	openItems, _ := layoutWalk("seed-open", start, fitStops(homePoint, route, backBy.Sub(start)))
	lock, six := at(days.tomorrow, 17, 0).UTC(), 6
	names := make([]string, len(route))
	for i, a := range route {
		names[i] = a.Name
	}
	w.openPlanSummary = fmt.Sprintf("%s %s (%s), %s", start.Format("Mon Jan 2"), httpx.TimeRange(start, backBy), loc, strings.Join(names, ", "))
	w.itineraries = append(w.itineraries, models.Itinerary{
		ID: seedOpenPlanID, HostID: ids.marin, MemberIDs: []string{ids.marin, ids.theo}, Title: "Golden hour by the market",
		DateKey: days.tomorrow.Format("2006-01-02"), TZ: loc.String(), Date: days.tomorrow.UTC(),
		Start: start.UTC(), BackBy: backBy.UTC(), StartPlace: homePlace(), EndPlace: homePlace(),
		Visibility: models.VisibilityOpen, LockAt: &lock, MaxGroupSize: &six, Items: openItems,
		Plan: &models.PlanSnapshot{Range: "walkable", Ride: "none", Tags: planTags(route), Budget: planBudget(route),
			Who: models.VisibilityOpen, Pace: "balanced", Modes: []string{"walk"}},
		RouteMode: "walk", Status: models.ItineraryActive, PostID: seedOpenPlanID,
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	})

	// The market crew: last Saturday morning, Sandy with Marin and Theo;
	// Sandy rated both stops. A finished plan has no Forum post, so it is
	// just_me whatever time filter the Forum applies.
	market, err := findPlace(places, "Seaside Market Hall", "market")
	if err != nil {
		return nil, err
	}
	coffee, err := findPlace(places, "Driftwood Coffee Roasters", "cafe")
	if err != nil {
		return nil, err
	}
	sat := days.lastSaturday
	crewItems, crewEnd := layoutWalk("seed-crew", at(sat, 9, 30), []seedStop{{market, 75}, {coffee, 45}})
	w.itineraries = append(w.itineraries, models.Itinerary{
		ID: seedCrewPlanID, HostID: ids.sandy, MemberIDs: crewMembers, Title: "Saturday market crew",
		DateKey: sat.Format("2006-01-02"), TZ: loc.String(), Date: sat.UTC(),
		Start: at(sat, 9, 30).UTC(), BackBy: halfHourCeil(crewEnd).UTC(), StartPlace: homePlace(), EndPlace: homePlace(),
		Visibility: models.VisibilityJustMe, Items: crewItems,
		Plan: &models.PlanSnapshot{Range: "walkable", Ride: "none", Tags: []string{"Food", "Chill"}, Budget: 1,
			Who: models.VisibilityFriends, Pace: "balanced", Modes: []string{"walk"}},
		RouteMode: "walk", Status: models.ItineraryPast, ThreadID: seedCrewThreadID,
		CreatedAt: at(sat, 8, 40).UTC(), UpdatedAt: at(sat, 8, 40).UTC(),
	})
	crewStops := stopsOf(crewItems)
	rated := at(sat, 18, 0).UTC()
	for i, r := range []struct {
		stars int
		tags  []string
	}{{5, []string{"Great people", "Would go again"}}, {4, []string{"Good value"}}} {
		stop := crewStops[i]
		w.ratings = append(w.ratings, models.Rating{
			ID: models.PairID(ids.sandy, stop.ID), UserID: ids.sandy, ItemID: stop.ID, ItineraryID: seedCrewPlanID,
			ActivityID: stop.ActivityID, Stars: r.stars, Tags: r.tags, CreatedAt: rated, UpdatedAt: rated,
		})
	}

	// The solo walk: yesterday afternoon, one stop still to rate.
	greenway, err := findPlace(places, "Heron Creek Greenway", "hike")
	if err != nil {
		return nil, err
	}
	day := days.yesterday
	soloItems, soloEnd := layoutWalk("seed-solo", at(day, 16, 0), []seedStop{{greenway, 90}})
	w.soloStopID = stopsOf(soloItems)[0].ID
	w.itineraries = append(w.itineraries, models.Itinerary{
		ID: seedSoloPlanID, HostID: ids.sandy, MemberIDs: []string{ids.sandy}, Title: "Heron Creek wander",
		DateKey: day.Format("2006-01-02"), TZ: loc.String(), Date: day.UTC(),
		Start: at(day, 16, 0).UTC(), BackBy: halfHourCeil(soloEnd).UTC(), StartPlace: homePlace(), EndPlace: homePlace(),
		Visibility: models.VisibilityJustMe, Items: soloItems,
		Plan: &models.PlanSnapshot{Range: "walkable", Ride: "none", Tags: []string{"Outdoors", "Chill"}, Budget: 0,
			Who: models.VisibilityJustMe, Pace: "relaxed", Modes: []string{"walk"}},
		RouteMode: "walk", Status: models.ItineraryPast,
		CreatedAt: at(day, 15, 20).UTC(), UpdatedAt: at(day, 15, 20).UTC(),
	})

	// The crew's chat (the group id is the thread id) and its two expenses,
	// each split equally three ways: Sandy owes Marin $5, Theo owes Marin $8
	// and Sandy $3.
	crewChat := []struct {
		id, from, text string
		at             time.Time
	}{
		{"seed-msg-crew-1", ids.marin, "Coffee's on me today, it's in Splits.", at(sat, 12, 14)},
		{"seed-msg-crew-2", ids.sandy, "Thank you! I added the bus fares.", at(sat, 12, 16)},
		{"seed-msg-crew-3", ids.theo, "Best Saturday in ages. Same time next week?", at(sat, 12, 21)},
	}
	for _, m := range crewChat {
		w.messages = append(w.messages, models.Message{ID: m.id, ThreadID: seedCrewThreadID, SenderID: m.from, Text: m.text, SentAt: m.at.UTC()})
	}
	lastCrew := crewChat[len(crewChat)-1]
	w.threads = append(w.threads, models.Thread{
		ID: seedCrewThreadID, IsGroup: true, Title: "Saturday market crew", MemberIDs: crewMembers, ItineraryID: seedCrewPlanID,
		CreatedBy: ids.sandy, LastMessageText: lastCrew.text, LastSenderID: lastCrew.from, LastMessageAt: lastCrew.at.UTC(),
		Unread:    map[string]int{ids.sandy: 0, ids.marin: 0, ids.theo: 0},
		ReadAt:    map[string]time.Time{ids.sandy: at(sat, 12, 30).UTC(), ids.marin: at(sat, 12, 30).UTC(), ids.theo: lastCrew.at.UTC()},
		CreatedAt: at(sat, 9, 30).UTC(), UpdatedAt: lastCrew.at.UTC(),
	})
	for _, e := range []struct {
		id, what, payer string
		cents           int
		at              time.Time
	}{
		{"seed-exp-coffee", "Coffee", ids.marin, 2400, at(sat, 12, 12)},
		{"seed-exp-bus-fares", "Bus fares", ids.sandy, 900, at(sat, 12, 15)},
	} {
		w.expenses = append(w.expenses, models.Expense{
			ID: e.id, GroupID: seedCrewThreadID, What: e.what, AmountCents: e.cents, PayerID: e.payer,
			SplitAmong: crewMembers, Shares: equalShares(e.cents, len(crewMembers)), CreatedBy: e.payer, CreatedAt: e.at.UTC(),
		})
	}

	// Sandy and Marin are friends with a DM: Marin's newer message is unread.
	friendsSince := now.AddDate(0, 0, -30)
	pair := []string{ids.sandy, ids.marin}
	sort.Strings(pair)
	w.friendships = append(w.friendships, models.Friendship{ID: models.FriendshipID(ids.sandy, ids.marin), UserIDs: pair, CreatedAt: friendsSince})
	dm1, dm2 := now.Add(-22*time.Hour), now.Add(-40*time.Minute)
	w.messages = append(w.messages,
		models.Message{ID: "seed-msg-dm-1", ThreadID: seedDMThreadID, SenderID: ids.sandy, SentAt: dm1,
			Text: "That market morning was the best. Let's do it again soon!"},
		models.Message{ID: "seed-msg-dm-2", ThreadID: seedDMThreadID, SenderID: ids.marin, SentAt: dm2,
			Text: "I'm hosting a golden hour walk tomorrow at 5:30. Join if you're free!"},
	)
	w.threads = append(w.threads, models.Thread{
		ID: seedDMThreadID, MemberIDs: []string{ids.sandy, ids.marin}, DMKey: models.DMKey(ids.sandy, ids.marin),
		CreatedBy: ids.sandy, LastMessageText: w.messages[len(w.messages)-1].Text, LastSenderID: ids.marin, LastMessageAt: dm2,
		Unread: map[string]int{ids.sandy: 1, ids.marin: 0}, ReadAt: map[string]time.Time{ids.sandy: dm1, ids.marin: dm2},
		CreatedAt: dm1, UpdatedAt: dm2,
	})

	// Theo asked to be friends.
	asked := now.Add(-3 * time.Hour)
	w.requests = append(w.requests, models.FriendRequest{
		ID: seedRequestID, FromID: ids.theo, ToID: ids.sandy, Note: "Met at the market", Status: models.RequestPending,
		CreatedAt: asked, UpdatedAt: asked,
	})

	// Marin is free now near the market (friends see it).
	until := freeUntil(now, loc)
	area := "Seaside Market"
	w.posts = append(w.posts, models.ForumPost{
		ID: seedFreePostID, Type: models.PostFreeNow, AuthorID: ids.marin, Visibility: models.PostVisibilityFriends,
		Location:  models.GeoJSONPoint{Type: "Point", Coordinates: []float64{market.Location.Coordinates[0], market.Location.Coordinates[1]}},
		AreaLabel: &area, Text: fmt.Sprintf("Free until %s near %s", httpx.ClockShort(until), area),
		Until: until.UTC(), ExpiresAt: until.UTC(), CreatedAt: now.Add(-10 * time.Minute),
	})

	// Sandy's demo card (nothing real behind it).
	w.cards = append(w.cards, models.PaymentMethod{
		ID: seedCardID, UserID: ids.sandy, Brand: "Visa", Last4: "4242", IsDefault: true, DemoToken: "tok_visa_4242", CreatedAt: friendsSince,
	})
	return w, nil
}

func homePlace() models.PlaceDoc {
	lat, lng := sandyHomeBase.Lat, sandyHomeBase.Lng
	return models.PlaceDoc{Name: sandyHomeBase.Name, Lat: &lat, Lng: &lng}
}

// freeUntil is when Marin's free-now post runs out: three hours from now
// and not before 9 PM, on the half hour.
func freeUntil(now time.Time, loc *time.Location) time.Time {
	local := now.In(loc)
	until := local.Add(3 * time.Hour)
	if evening := at(local, 21, 0); until.Before(evening) {
		until = evening
	}
	return halfHourCeil(until)
}

// halfHourCeil rounds a local time up to :00 or :30.
func halfHourCeil(t time.Time) time.Time {
	rounded := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()/30*30, 0, 0, t.Location())
	if rounded.Before(t) {
		rounded = rounded.Add(30 * time.Minute)
	}
	return rounded
}

// equalShares splits cents n ways: floor each, the first cents mod n pay 1¢ more.
func equalShares(cents, n int) []int {
	shares := make([]int, n)
	for i := range shares {
		shares[i] = cents / n
		if i < cents%n {
			shares[i]++
		}
	}
	return shares
}

// seedStop is a catalog place and the minutes spent there.
type seedStop struct {
	place   *models.Activity
	minutes int
}

// walkOrder visits places nearest-first from p.
func walkOrder(p travel.Point, places []*models.Activity) []*models.Activity {
	left := append([]*models.Activity(nil), places...)
	var route []*models.Activity
	for len(left) > 0 {
		next := nearestPlaces(left, p, 1)[0]
		route = append(route, next)
		p = placePoint(next)
		for i, a := range left {
			if a == next {
				left = append(left[:i], left[i+1:]...)
				break
			}
		}
	}
	return route
}

// fitStops shares the window, less the walking, among the stops in
// proportion to their usual visit length, in 5-minute steps (15 at least).
func fitStops(base travel.Point, route []*models.Activity, window time.Duration) []seedStop {
	walking, usual := 0, 0.0
	from := base
	for _, a := range route {
		walking += walkMinutes(from, placePoint(a))
		usual += usualMinutes(a)
		from = placePoint(a)
	}
	walking += walkMinutes(from, base)
	left := int(window.Minutes()) - walking
	stops := make([]seedStop, len(route))
	for i, a := range route {
		minutes := int(math.Floor(usualMinutes(a)/usual*float64(left)/5)) * 5
		stops[i] = seedStop{place: a, minutes: max(minutes, 15)}
	}
	return stops
}

func usualMinutes(a *models.Activity) float64 {
	if a.Duration != nil && a.Duration.MedianMin > 0 {
		return a.Duration.MedianMin
	}
	return 60
}

// walkMinutes is the heuristic walk between two points (at least a minute).
func walkMinutes(a, b travel.Point) int {
	return max(int(travel.Estimate(a, b, travel.Walk).Duration.Minutes()), 1)
}

// layoutWalk lays out a walking plan from the home base: a walk to each
// stop, the stop, and a walk back. It returns the items and when the walk
// back ends. Item ids are prefix-leg-N / prefix-stop-N.
func layoutWalk(prefix string, start time.Time, stops []seedStop) ([]models.ItineraryItem, time.Time) {
	var items []models.ItineraryItem
	cursor, from, fromName := start, homePoint, sandyHomeBase.Name
	leg := func(n int, to travel.Point, toName, title string) {
		minutes := walkMinutes(from, to)
		end := cursor.Add(time.Duration(minutes) * time.Minute)
		items = append(items, models.ItineraryItem{
			ID: fmt.Sprintf("%s-leg-%d", prefix, n), Kind: models.ItemTransit, Title: "Walk to " + title,
			Place: &models.PlaceDoc{Name: fromName + " → " + toName}, Start: cursor.UTC(), End: end.UTC(),
			Description: walkNote, LegMode: "walk", LegMinutes: minutes, DurationMin: minutes,
		})
		cursor = end
	}
	for i, stop := range stops {
		a := stop.place
		to, name := placePoint(a), activityPlaceName(a)
		leg(i+1, to, name, a.Name)
		end := cursor.Add(time.Duration(stop.minutes) * time.Minute)
		lat, lng := to.Lat, to.Lng
		item := models.ItineraryItem{
			ID: fmt.Sprintf("%s-stop-%d", prefix, i+1), Kind: models.ItemStop, Title: a.Name,
			Place: &models.PlaceDoc{Name: name, Lat: &lat, Lng: &lng}, Start: cursor.UTC(), End: end.UTC(),
			Description: activitySummary(a), WebsiteURL: a.URL, ActivityID: a.ID.Hex(), DurationMin: stop.minutes,
		}
		if cents, known := activityCents(a); known {
			item.PriceCents = &cents
		}
		item.Bookable = (a.TicketURL != nil && *a.TicketURL != "") || (item.PriceCents != nil && *item.PriceCents > 0)
		items = append(items, item)
		cursor, from, fromName = end, to, name
	}
	leg(len(stops)+1, homePoint, sandyHomeBase.Name, sandyHomeBase.Name)
	return items, cursor
}

// stopsOf are the stop items of a plan, in order.
func stopsOf(items []models.ItineraryItem) []models.ItineraryItem {
	var stops []models.ItineraryItem
	for _, item := range items {
		if item.Kind == models.ItemStop {
			stops = append(stops, item)
		}
	}
	return stops
}

// activityPlaceName is the venue, else the activity's own name.
func activityPlaceName(a *models.Activity) string {
	if a.VenueName != nil && strings.TrimSpace(*a.VenueName) != "" {
		return strings.TrimSpace(*a.VenueName)
	}
	return a.Name
}

func activitySummary(a *models.Activity) string {
	if a.Summary != nil {
		return strings.TrimSpace(*a.Summary)
	}
	return ""
}

// activityCents is the entry price as the planner reads it: the minimum in
// whole dollars, free, or a stored cents value; known is false otherwise.
func activityCents(a *models.Activity) (int, bool) {
	p := a.Price
	switch {
	case p == nil:
		return 0, false
	case p.Min != nil:
		return max(int(math.Round(*p.Min*100)), 0), true
	case p.IsFree:
		return 0, true
	case p.Cents > 0:
		return int(p.Cents), true
	}
	return 0, false
}

// planTags are the Create-flow quick picks the stops fit.
func planTags(route []*models.Activity) []string {
	has := map[string]bool{}
	for _, a := range route {
		tags := map[string]bool{}
		for _, t := range a.Tags {
			tags[t] = true
		}
		if tags["outdoor"] || a.Category == "park" || a.Category == "hike" || a.Category == "garden" {
			has["Outdoors"] = true
		}
		if tags["food"] || a.Category == "market" || a.Category == "restaurant" || a.Category == "cafe" {
			has["Food"] = true
		}
		if tags["art"] || a.Category == "gallery" || a.Category == "museum" {
			has["Art"] = true
		}
		if tags["music"] || a.Category == "live_music" {
			has["Music"] = true
		}
	}
	var out []string
	for _, tag := range []string{"Outdoors", "Food", "Art", "Music"} {
		if has[tag] {
			out = append(out, tag)
		}
	}
	return out
}

// planBudget is the highest price tier among the stops (0 Free … 3 $$$).
func planBudget(route []*models.Activity) int {
	budget := 0
	for _, a := range route {
		if a.Price != nil {
			budget = max(budget, min(a.Price.Tier, 3))
		}
	}
	return budget
}

// ---- writing -------------------------------------------------------------------

// adoptExistingDM points the seeded DM at the Sandy–Marin thread when one
// already exists under another id (dmKey is unique).
func adoptExistingDM(ctx context.Context, st *store.Store, w *seedWorld) error {
	var dm *models.Thread
	for i := range w.threads {
		if w.threads[i].DMKey != "" {
			dm = &w.threads[i]
		}
	}
	if dm == nil {
		return nil
	}
	var existing models.Thread
	err := st.Collection(store.CollThreads).FindOne(ctx, bson.M{"dmKey": dm.DMKey}).Decode(&existing)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("look up the Sandy–Marin DM: %w", err)
	}
	if existing.ID == dm.ID {
		return nil
	}
	for i := range w.messages {
		if w.messages[i].ThreadID == dm.ID {
			w.messages[i].ThreadID = existing.ID
		}
	}
	dm.ID = existing.ID
	return nil
}

// seedDoc is one document to write under its fixed id.
type seedDoc struct {
	id  string
	doc any
}

func docsOf[T any](items []T, id func(*T) string) []seedDoc {
	docs := make([]seedDoc, len(items))
	for i := range items {
		docs[i] = seedDoc{id: id(&items[i]), doc: &items[i]}
	}
	return docs
}

// writeSeedWorld replaces every seeded document (inserting the missing
// ones) and undoes what a walkthrough changes that the seed does not own:
// another default card of Sandy's, her rating of the solo stop and an
// accepted friendship with Theo. It returns how many documents of each
// collection the demo has.
func writeSeedWorld(ctx context.Context, st *store.Store, w *seedWorld) ([]seedCount, error) {
	// One default card per user (a unique partial index): another card Sandy
	// made the default stops being it before the demo Visa takes over.
	if _, err := st.Collection(store.CollPaymentMethods).UpdateMany(ctx,
		bson.M{"userId": w.sandyID, "_id": bson.M{"$ne": seedCardID}, "isDefault": true}, bson.M{"$set": bson.M{"isDefault": false}}); err != nil {
		return nil, fmt.Errorf("make room for the demo Visa as the default: %w", err)
	}
	collections := []struct {
		name string
		docs []seedDoc
	}{
		{store.CollItineraries, docsOf(w.itineraries, func(d *models.Itinerary) string { return d.ID })},
		{store.CollThreads, docsOf(w.threads, func(d *models.Thread) string { return d.ID })},
		{store.CollMessages, docsOf(w.messages, func(d *models.Message) string { return d.ID })},
		{store.CollExpenses, docsOf(w.expenses, func(d *models.Expense) string { return d.ID })},
		{store.CollRatings, docsOf(w.ratings, func(d *models.Rating) string { return d.ID })},
		{store.CollFriendships, docsOf(w.friendships, func(d *models.Friendship) string { return d.ID })},
		{store.CollFriendRequests, docsOf(w.requests, func(d *models.FriendRequest) string { return d.ID })},
		{store.CollForumPosts, docsOf(w.posts, func(d *models.ForumPost) string { return d.ID })},
		{store.CollPaymentMethods, docsOf(w.cards, func(d *models.PaymentMethod) string { return d.ID })},
	}
	counts := []seedCount{{Collection: store.CollUsers, N: 3}}
	for _, coll := range collections {
		for _, d := range coll.docs {
			_, err := st.Collection(coll.name).ReplaceOne(ctx, bson.M{"_id": d.id}, d.doc, options.Replace().SetUpsert(true))
			if err != nil {
				return nil, fmt.Errorf("write %s %s: %w", coll.name, d.id, err)
			}
		}
		counts = append(counts, seedCount{Collection: coll.name, N: len(coll.docs)})
	}
	if _, err := st.Collection(store.CollRatings).DeleteOne(ctx, bson.M{"_id": models.PairID(w.sandyID, w.soloStopID)}); err != nil {
		return nil, fmt.Errorf("reset the unrated stop: %w", err)
	}
	if _, err := st.Collection(store.CollFriendships).DeleteOne(ctx, bson.M{"_id": models.FriendshipID(w.sandyID, w.theoID)}); err != nil {
		return nil, fmt.Errorf("reset Theo's request: %w", err)
	}
	return counts, nil
}
