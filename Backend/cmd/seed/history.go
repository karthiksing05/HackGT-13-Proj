package main

import (
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/travel"
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

// History mode (--history @handle,…) gives real accounts a believable
// past: finished sidequests over the last three weeks from real
// pitch_activities places (some with showcase people), ratings of some of
// their stops made through the app's own rating handler, and liked
// categories that agree with them, after which their taste vectors are
// rebuilt. Everything it creates is tagged seed: "history-v1" with the
// person's id; before it changes anything on their account it saves the
// fields it may change once, so --remove can put them back.
const (
	historyTag   = "history-v1"
	fieldFor     = "seedFor"
	collBackups  = "seed_backups" // the originals of each person (the app never reads it)
	historyReach = 3.0            // miles: how far a past plan's first stop may be from its area
)

// minUnrated is how many stops of the history stay unrated at least, so
// Home's "past events to rate" card has something to show.
const minUnrated = 5

// pastStop is one stop of a past plan and how the person rated it (stars
// 0: left unrated).
type pastStop struct {
	stopSpec
	stars int
	tags  []string
	note  string
}

// pastPlan is one finished sidequest daysAgo days before the history's
// anchor (the first --apply), starting at start (local).
type pastPlan struct {
	key        string
	title      string
	mood       string
	daysAgo    int
	start      [2]int
	anchor     travel.Point
	members    []string // showcase people (atlanta cast keys) who came along
	visibility string
	stops      []pastStop
	// names are the stops (0-based) the title names: it holds only when
	// those are the named places (a theme title only needs each stop's kind).
	names []int
}

// flavor is one story a history tells: its plans and the liked
// categories (preference ratings) they support.
type flavor struct {
	key, about string
	likes      map[string]int
	plans      []pastPlan
}

func rated(stars int, tags []string, note string) func(stopSpec) pastStop {
	return func(s stopSpec) pastStop { return pastStop{stopSpec: s, stars: stars, tags: tags, note: note} }
}

func unrated(s stopSpec) pastStop { return pastStop{stopSpec: s} }

func stop(minutes int, categories []string, names ...string) stopSpec {
	return stopSpec{names: names, categories: categories, minutes: minutes}
}

var (
	again     = []string{"Would go again"}
	crew      = []string{"Great people", "Would go again"}
	goodValue = []string{"Good value"}
	parks     = []string{"park", "garden", "hike"}
	eats      = []string{"restaurant", "market", "cafe"}
	arts      = []string{"museum", "gallery", "landmark"}
	night     = []string{"bar", "nightclub"}
	games     = []string{"rec_venue"}

	virginiaHighland = travel.Point{Lat: 33.7771, Lng: -84.3527}
	downtown         = travel.Point{Lat: 33.7580, Lng: -84.3880}
	sweetAuburn      = travel.Point{Lat: 33.7560, Lng: -84.3730}
	emory            = travel.Point{Lat: 33.7903, Lng: -84.3243}
	cabbagetown      = travel.Point{Lat: 33.7470, Lng: -84.3550}
	freedomPark      = travel.Point{Lat: 33.7699, Lng: -84.3483}
	midtownNorth     = travel.Point{Lat: 33.8100, Lng: -84.3720}
)

// flavors are the three stories, in the order a best fit prefers them.
var flavors = []flavor{
	{
		key: "outdoors", about: "parks, the BeltLine and food halls",
		likes: map[string]int{"outdoors": 5, "long_walks": 4, "food": 4, "early_mornings": 4},
		plans: []pastPlan{
			{key: "garden", title: "Piedmont Park and the Botanical Garden", mood: "a slow morning outside",
				daysAgo: 20, start: [2]int{10, 0}, anchor: piedmontPark, members: []string{"diego"}, visibility: models.VisibilityFriends, names: []int{0, 1},
				stops: []pastStop{
					rated(5, crew, "Got there before the crowds, the lake was glassy")(stop(60, parks, "Piedmont Park")),
					rated(4, again, "The canopy walk is worth the ticket")(stop(90, []string{"garden", "park"}, "Atlanta Botanical Garden")),
					rated(4, goodValue, "")(stop(60, eats, "Sugar Factory American Brasserie")),
				}},
			{key: "eastside", title: "Eastside Trail to Ponce City Market", mood: "walk the BeltLine, eat at the food hall",
				daysAgo: 16, start: [2]int{10, 30}, anchor: ponceCityMarket, members: []string{"sofia", "diego"}, visibility: models.VisibilityFriends, names: []int{0, 1},
				stops: []pastStop{
					rated(5, crew, "")(stop(75, []string{"hike", "park"}, "Atlanta Beltline Eastside Trail")),
					rated(4, []string{"Too crowded", "Would go again"}, "Lunch rush at the food hall, go before noon")(stop(75, []string{"market", "restaurant"}, "Ponce City Market")),
					rated(5, again, "")(stop(45, parks, "Historic Fourth Ward Park")),
				}},
			{key: "freedom", title: "Freedom Park to Candler Park", mood: "a long walk and an early dinner",
				daysAgo: 12, start: [2]int{15, 30}, anchor: freedomPark, visibility: models.VisibilityJustMe, names: []int{0, 1},
				stops: []pastStop{
					rated(4, again, "")(stop(60, parks, "Freedom Park")),
					rated(4, goodValue, "")(stop(45, parks, "Candler Park")),
					unrated(stop(75, eats, "The Porter Beer Bar", "North Highland Pub")),
				}},
			{key: "aquarium", title: "Centennial Park and the Aquarium", mood: "a downtown morning",
				daysAgo: 6, start: [2]int{9, 0}, anchor: downtown, members: []string{"lily"}, visibility: models.VisibilityFriends, names: []int{0, 1},
				stops: []pastStop{
					unrated(stop(45, parks, "Centennial Olympic Park")),
					unrated(stop(120, []string{"zoo_aquarium", "museum"}, "Georgia Aquarium")),
				}},
			{key: "fourthward", title: "Golden hour at Historic Fourth Ward Park", mood: "sunset, games, a patio",
				daysAgo: 2, start: [2]int{17, 0}, anchor: ponceCityMarket, members: []string{"diego", "sofia"}, visibility: models.VisibilityFriends, names: []int{0},
				stops: []pastStop{
					unrated(stop(45, parks, "Historic Fourth Ward Park")),
					unrated(stop(75, games, "Skyline Park")),
					unrated(stop(75, eats, "New Realm Brewing Co.")),
				}},
		},
	},
	{
		key: "nightlife", about: "live music, nights out and late bites",
		likes: map[string]int{"live_music": 5, "nightlife": 5, "food": 4, "big_crowds": 4},
		plans: []pastPlan{
			{key: "blues", title: "Blues in Virginia-Highland", mood: "wings, then the blues",
				daysAgo: 19, start: [2]int{19, 30}, anchor: virginiaHighland, members: []string{"jalen"}, visibility: models.VisibilityFriends,
				stops: []pastStop{
					rated(4, goodValue, "")(stop(60, eats, "Moe's and Joe's")),
					rated(5, again, "The house band played until close")(stop(120, night, "Blind Willie's")),
				}},
			{key: "duckpin", title: "Duckpin bowling, then Northside Tavern", mood: "games and a blues bar",
				daysAgo: 15, start: [2]int{19, 0}, anchor: westsideAnchor, members: []string{"marcus", "amara"}, visibility: models.VisibilityFriends, names: []int{1},
				stops: []pastStop{
					rated(5, crew, "")(stop(90, games, "The Painted Duck")),
					rated(4, again, "")(stop(120, night, "Northside Tavern")),
				}},
			{key: "downtown", title: "Dinner and dancing downtown", mood: "a big night out",
				daysAgo: 11, start: [2]int{19, 30}, anchor: downtown, members: []string{"jalen", "marcus"}, visibility: models.VisibilityFriends,
				stops: []pastStop{
					rated(3, []string{"Too pricey"}, "")(stop(75, eats, "42 Bar and Grill")),
					rated(5, crew, "Best DJ set we've seen this year")(stop(120, []string{"nightclub", "bar"}, "Lore")),
					unrated(stop(90, []string{"nightclub", "bar"}, "Royal Peacock Lounge")),
				}},
			{key: "pickle", title: "Pickleball and bowling up Piedmont", mood: "games night",
				daysAgo: 7, start: [2]int{19, 0}, anchor: midtownNorth, members: []string{"amara"}, visibility: models.VisibilityFriends, names: []int{0, 1},
				stops: []pastStop{
					unrated(stop(60, games, "Painted Pickle")),
					unrated(stop(90, games, "Midtown Bowl")),
				}},
			{key: "pubs", title: "Midtown pub night", mood: "three stops, all walkable",
				daysAgo: 3, start: [2]int{18, 30}, anchor: midtown, members: []string{"jalen"}, visibility: models.VisibilityFriends,
				stops: []pastStop{
					unrated(stop(60, eats, "Sugar Factory American Brasserie")),
					unrated(stop(75, []string{"restaurant", "bar"}, "Eleventh Street Pub")),
					unrated(stop(60, []string{"restaurant", "bar"}, "Fado Irish Pub")),
				}},
		},
	},
	{
		key: "arts", about: "museums, galleries and long walks",
		likes: map[string]int{"museums": 5, "long_walks": 4, "shopping": 3, "food": 3},
		plans: []pastPlan{
			{key: "high", title: "Afternoon at the High Museum", mood: "art, design, a walk after",
				daysAgo: 18, start: [2]int{12, 30}, anchor: piedmontPark, members: []string{"hana"}, visibility: models.VisibilityFriends, names: []int{0},
				stops: []pastStop{
					rated(5, again, "The folk art rooms were my favorite part")(stop(120, arts, "High Museum of Art")),
					rated(4, goodValue, "")(stop(60, arts, "MODA (Museum of Design Atlanta)")),
					rated(4, again, "")(stop(45, parks, "Piedmont Park")),
				}},
			{key: "auburn", title: "Sweet Auburn history walk", mood: "history, then the tunnel murals",
				daysAgo: 14, start: [2]int{13, 0}, anchor: sweetAuburn, visibility: models.VisibilityJustMe,
				stops: []pastStop{
					rated(5, again, "Go early, the house tour fills up")(stop(90, arts, "Martin Luther King, Jr. National Historical Park")),
					rated(5, nil, "")(stop(45, arts, "The King Center")),
					rated(4, goodValue, "")(stop(30, []string{"landmark", "gallery"}, "Krog Street Tunnel")),
				}},
			{key: "carlos", title: "The Carlos Museum and Lullwater", mood: "antiquities and a quiet trail",
				daysAgo: 10, start: [2]int{11, 0}, anchor: emory, members: []string{"omar"}, visibility: models.VisibilityFriends, names: []int{0, 1},
				stops: []pastStop{
					rated(4, goodValue, "")(stop(90, arts, "Michael C. Carlos Museum")),
					unrated(stop(60, parks, "Lullwater Preserve")),
				}},
			{key: "cabbagetown", title: "Galleries and the Krog Street Tunnel", mood: "street art, then dinner",
				daysAgo: 5, start: [2]int{14, 0}, anchor: cabbagetown, members: []string{"hana", "lily"}, visibility: models.VisibilityFriends, names: []int{1},
				stops: []pastStop{
					unrated(stop(60, arts, "ABV Gallery")),
					unrated(stop(30, []string{"landmark", "gallery"}, "Krog Street Tunnel")),
					unrated(stop(60, eats, "Milltown Arms Tavern")),
				}},
			{key: "puppets", title: "Puppetry Arts, then Piedmont Park", mood: "a museum morning and a walk",
				daysAgo: 2, start: [2]int{10, 30}, anchor: piedmontPark, visibility: models.VisibilityJustMe, names: []int{0, 1},
				stops: []pastStop{
					unrated(stop(90, arts, "Center For Puppetry Arts")),
					unrated(stop(60, parks, "Piedmont Park")),
				}},
		},
	},
}

func flavorByKey(key string) *flavor {
	for i := range flavors {
		if flavors[i].key == key {
			return &flavors[i]
		}
	}
	return nil
}

// historySel is one --history entry: a handle, and the story to tell for
// them ("" = the one that best fits what they already like).
type historySel struct {
	handle, flavor string
}

// parseHistory reads "@lilk,@bayan_98d1=nightlife,…".
func parseHistory(list string) ([]historySel, error) {
	var out []historySel
	for part := range strings.SplitSeq(list, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		handle, fl, _ := strings.Cut(part, "=")
		handle = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
		fl = strings.TrimSpace(fl)
		if handle == "" {
			return nil, fmt.Errorf("--history: %q has no handle", part)
		}
		if fl != "" && flavorByKey(fl) == nil {
			return nil, fmt.Errorf("--history: unknown story %q for @%s (outdoors, nightlife or arts)", fl, handle)
		}
		for _, s := range out {
			if s.handle == handle {
				return nil, fmt.Errorf("--history: @%s is listed twice", handle)
			}
		}
		out = append(out, historySel{handle: handle, flavor: fl})
	}
	if len(out) == 0 {
		return nil, errors.New("--history needs at least one @handle")
	}
	return out, nil
}

// historyFields are the account fields a history run may change: the
// preferences it merges into, the taste tags its ratings move (the rating
// handler), the taste vectors and profile bookkeeping it rebuilds
// (pkg/profiles, or the blend) and updatedAt. They are saved once, before
// the first change.
var historyFields = []string{"prefs", "taste", "positiveText", "negativeText", "positiveEmbedding", "negativeEmbedding",
	"embeddingModel", "profileTextHash", "profileInputHash", "profileUpdatedAt", "embedding", "updatedAt"}

// vectorFields are restored together or not at all.
var vectorFields = []string{"positiveText", "negativeText", "positiveEmbedding", "negativeEmbedding", "embeddingModel",
	"profileTextHash", "profileInputHash", "profileUpdatedAt", "embedding"}

// backup is a person's seed_backups document.
type backup struct {
	ID       string    `bson:"_id"`
	Seed     string    `bson:"seed"`
	For      string    `bson:"seedFor"`
	Username string    `bson:"username"`
	Flavor   string    `bson:"flavor"`
	SavedAt  time.Time `bson:"savedAt"`
	// Anchor is "now" of the first --apply: the history's dates hang off it,
	// so a rerun rewrites the same plans.
	Anchor time.Time `bson:"anchor"`
	// Original holds the fields as they were before the first change
	// (Missing names the absent ones); Seeded as the last --apply left them.
	Original       bson.Raw `bson:"original"`
	Missing        []string `bson:"missing"`
	Seeded         bson.Raw `bson:"seeded,omitempty"`
	SeededMissing  []string `bson:"seededMissing,omitempty"`
	RatingsChanged []string `bson:"ratingsChanged,omitempty"` // the preference keys it filled or raised
}

func backupID(uid string) string { return historyTag + "|" + uid }

// histPerson is one person of a history run.
type histPerson struct {
	upcoming []upcomingDoc // the default upcoming sidequests of their baseline
	upNotes  []string
	sel      historySel
	user     *models.User
	raw      bson.Raw // their account as stored
	flavor   *flavor
	fitNote  string
	backup   *backup // nil before the first --apply
	anchor   time.Time
	plans    []histPlan
	ratings  []histRating
	likes    map[string]int // the merged preference ratings
	changed  []string       // keys filled or raised, sorted
	taste    string         // how the taste vectors will be made
	pos      []float64
	neg      []float64
	notes    []string
}

// histPlan is one past sidequest to write.
type histPlan struct {
	doc     *models.Itinerary
	act     action
	note    string
	members []string // names of who came along
}

// histRating is one stop to rate through the rating handler.
type histRating struct {
	planID, itemID string
	stars          int
	tags           []string
	note           string
}

func (hp *histPerson) uid() string { return hp.user.ID.Hex() }

// resolveHistory finds each person and refuses anyone who is not a real
// regular account: no match, more than one, the demo cast or the showcase.
func resolveHistory(ctx context.Context, st *store.Store, sels []historySel) ([]*histPerson, error) {
	return resolveAccounts(ctx, st, sels, false)
}

// resolveAccounts is resolveHistory that, with allowDemo, lets the demo
// account through (never a bot or a showcase account).
func resolveAccounts(ctx context.Context, st *store.Store, sels []historySel, allowDemo bool) ([]*histPerson, error) {
	var out []*histPerson
	var problems []string
	for _, sel := range sels {
		cursor, err := st.Collection(store.CollUsers).Find(ctx, bson.M{"usernameLower": sel.handle}, options.Find().SetLimit(2))
		if err != nil {
			return nil, fmt.Errorf("look up @%s: %w", sel.handle, err)
		}
		var raws []bson.Raw
		if err := cursor.All(ctx, &raws); err != nil {
			return nil, fmt.Errorf("look up @%s: %w", sel.handle, err)
		}
		if len(raws) != 1 {
			problems = append(problems, fmt.Sprintf("@%s matches %d accounts", sel.handle, len(raws)))
			continue
		}
		var u storedUser
		if err := bson.Unmarshal(raws[0], &u); err != nil {
			return nil, fmt.Errorf("read @%s: %w", sel.handle, err)
		}
		switch {
		case allowDemo && u.HasRole("demo") && !u.HasRole("bot"):
		case store.IsDemoCast(&u.User):
			problems = append(problems, fmt.Sprintf("@%s is in the demo cast (roles %v)", sel.handle, u.Roles))
			continue
		case u.HasRole("showcase") || u.Seed != "":
			problems = append(problems, fmt.Sprintf("@%s is a seeded showcase account", sel.handle))
			continue
		}
		out = append(out, &histPerson{sel: sel, user: &u.User, raw: raws[0]})
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("refusing: %s", strings.Join(problems, "; "))
	}
	return out, nil
}

// pickFlavors gives each person their story: the one their history already
// tells (a rerun), else the one named, else the one whose liked categories
// best match what they already rated (each story once while there are
// unused ones). Naming another story than an existing history's is refused.
func pickFlavors(people []*histPerson) error {
	taken := map[string]bool{}
	for _, hp := range people {
		if hp.backup != nil && flavorByKey(hp.backup.Flavor) != nil {
			if hp.sel.flavor != "" && hp.sel.flavor != hp.backup.Flavor {
				return fmt.Errorf("@%s already has the %s history; remove it first (--remove --apply) to tell another", hp.sel.handle, hp.backup.Flavor)
			}
			hp.sel.flavor = hp.backup.Flavor
		}
		if hp.sel.flavor != "" {
			taken[hp.sel.flavor] = true
		}
	}
	for _, hp := range people {
		if hp.sel.flavor != "" {
			hp.flavor = flavorByKey(hp.sel.flavor)
			hp.fitNote = "chosen with --history"
			if hp.backup != nil {
				hp.fitNote = "as on the first --apply"
			}
			continue
		}
		best, bestScore := -1, -1
		for i, fl := range flavors {
			if taken[fl.key] && len(taken) < len(flavors) {
				continue
			}
			score := 0
			for key := range fl.likes {
				score += hp.user.Prefs.Ratings[key]
			}
			if score > bestScore {
				best, bestScore = i, score
			}
		}
		hp.flavor = &flavors[best]
		taken[hp.flavor.key] = true
		var have []string
		for _, key := range contract.TripTypes {
			if _, ok := hp.flavor.likes[key]; ok && hp.user.Prefs.Ratings[key] > 0 {
				have = append(have, fmt.Sprintf("%s %d", key, hp.user.Prefs.Ratings[key]))
			}
		}
		hp.fitNote = "the best fit for what they already like"
		if len(have) > 0 {
			hp.fitNote += " (" + strings.Join(have, ", ") + ")"
		}
	}
	return nil
}

// loadBackup is the person's saved originals, nil before the first --apply.
func loadBackup(ctx context.Context, st *store.Store, uid string) (*backup, error) {
	var b backup
	err := st.Collection(collBackups).FindOne(ctx, bson.M{"_id": backupID(uid)}).Decode(&b)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the backup of %s: %w", uid, err)
	}
	return &b, nil
}

// showcasePeople are the Atlanta showcase people present in the database,
// by cast key: the history takes them along only when they exist.
func showcasePeople(ctx context.Context, st *store.Store) (map[string]*models.User, error) {
	ids := make([]any, 0, len(atlanta.cast))
	for _, p := range atlanta.cast {
		ids = append(ids, p.id)
	}
	cursor, err := st.Collection(store.CollUsers).Find(ctx, bson.M{"_id": bson.M{"$in": ids}, fieldSeed: seedTag})
	if err != nil {
		return nil, fmt.Errorf("read the showcase people: %w", err)
	}
	var users []*models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, fmt.Errorf("read the showcase people: %w", err)
	}
	out := map[string]*models.User{}
	for _, p := range atlanta.cast {
		for _, u := range users {
			if u.ID == p.id {
				out[p.key] = u
			}
		}
	}
	return out, nil
}

// planHistory lays out one person's history as of their anchor.
func planHistory(ctx context.Context, st *store.Store, hp *histPerson, cat *catalog, crew map[string]*models.User, o Options) error {
	hp.anchor = o.Now
	if hp.backup != nil && !hp.backup.Anchor.IsZero() {
		hp.anchor = hp.backup.Anchor
	}
	loc := o.Atlanta
	var ids []any
	for _, p := range hp.flavor.plans {
		ids = append(ids, historyPlanID(hp.uid(), p.key))
	}
	have, err := existingTagged(ctx, st, store.CollItineraries, ids, historyTag)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for i := range hp.flavor.plans {
		p := &hp.flavor.plans[i]
		local := hp.anchor.In(loc)
		start := time.Date(local.Year(), local.Month(), local.Day()-p.daysAgo, p.start[0], p.start[1], 0, 0, loc)
		specs := make([]stopSpec, len(p.stops))
		for j, s := range p.stops {
			specs[j] = s.stopSpec
		}
		reach := &worldSpec{center: p.anchor, maxFromCenter: historyReach}
		lay, ok := cat.layOutAt(reach, p.anchor, specs, start, loc, used)
		switch {
		case !ok:
			hp.notes = append(hp.notes, fmt.Sprintf("“%s” left out: nothing in %s was open for it near where it met", p.title, store.DefaultCatalog))
			continue
		case !lay.backBy.Before(o.Now):
			hp.notes = append(hp.notes, fmt.Sprintf("“%s” left out: it would not have ended yet", p.title))
			continue
		}
		plan, names := hp.planDoc(p, lay, crew, loc)
		hplan := histPlan{doc: plan, members: names}
		hplan.act, hplan.note = classify(have, plan.ID)
		hp.plans = append(hp.plans, hplan)
		for k, slot := range lay.slots {
			s := p.stops[slot]
			if s.stars == 0 {
				continue
			}
			r := histRating{planID: plan.ID, itemID: stopItem(plan.ID, k+1, lay.stops[k]).ID, stars: s.stars, tags: s.tags, note: s.note}
			if lay.stops[k].tier > 0 {
				r.note = "" // the note is about the named place, not the one that stood in for it
			}
			hp.ratings = append(hp.ratings, r)
		}
	}
	hp.keepUnrated()
	hp.mergeLikes()
	return nil
}

func historyPlanID(uid, key string) string { return fmt.Sprintf("history-%s-%s", uid, key) }

// planDoc is a finished plan as the itineraries collection keeps one: the
// person hosts, the showcase people who exist came along.
func (hp *histPerson) planDoc(p *pastPlan, lay laidOut, crew map[string]*models.User, loc *time.Location) (*models.Itinerary, []string) {
	planID := historyPlanID(hp.uid(), p.key)
	members := []string{hp.uid()}
	var names []string
	for _, key := range p.members {
		if u := crew[key]; u != nil {
			members = append(members, u.ID.Hex())
			names = append(names, u.Name)
		}
	}
	title := p.title
	if !titleHolds(p, lay) {
		title = "Afternoon at " + lay.stops[0].place.Name
		if h := lay.start.In(loc).Hour(); h >= 17 {
			title = "Evening at " + lay.stops[0].place.Name
		} else if h < 12 {
			title = "Morning at " + lay.stops[0].place.Name
		}
	}
	visibility := p.visibility
	if len(members) == 1 {
		visibility = models.VisibilityJustMe
	}
	startLocal := lay.start.In(loc)
	date := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	created := lay.start.Add(-time.Duration(20+p.daysAgo) * time.Hour).UTC()
	range_ := contract.RangeWalkable
	plan := &models.Itinerary{
		ID: planID, HostID: hp.uid(), MemberIDs: members, Title: title,
		DateKey: date.Format("2006-01-02"), TZ: loc.String(), Date: date.UTC(),
		Start: lay.start.UTC(), BackBy: lay.backBy.UTC(),
		StartPlace: placeDoc(lay.stops[0].place), EndPlace: placeDoc(lay.stops[len(lay.stops)-1].place),
		Visibility: visibility, Items: items(planID, lay.stops),
		Plan: &models.PlanSnapshot{Range: string(range_), Ride: string(contract.RideNone), MoodText: p.mood,
			Tags: planTags(lay.stops), Budget: planBudget(lay.stops), Who: visibility, Pace: string(contract.PaceBalanced),
			Modes: []string{string(contract.ModeWalk)}},
		RouteMode: string(contract.ModeWalk), Status: models.ItineraryPast,
		CreatedAt: created, UpdatedAt: lay.backBy.UTC(),
	}
	return plan, names
}

// titleHolds reports whether a plan's title is still true of its stops:
// every stop is there as its kind of place, and the ones the title names
// are those very places.
func titleHolds(p *pastPlan, lay laidOut) bool {
	return lay.exact && titleNamesHold(p.names, lay)
}

// keepUnrated leaves at least minUnrated stops unrated: when places had
// to be left out, the most recent ratings give way.
func (hp *histPerson) keepUnrated() {
	stops := 0
	for _, p := range hp.plans {
		for _, item := range p.doc.Items {
			if item.Kind == models.ItemStop {
				stops++
			}
		}
	}
	for stops-len(hp.ratings) < minUnrated && len(hp.ratings) > 0 {
		hp.ratings = hp.ratings[:len(hp.ratings)-1]
	}
}

// mergeLikes fills the story's liked categories the person has not rated
// and raises the ones rated lower; nothing they set is lowered or removed.
func (hp *histPerson) mergeLikes() {
	hp.likes = maps.Clone(hp.user.Prefs.Ratings)
	if hp.likes == nil {
		hp.likes = map[string]int{}
	}
	hp.changed = nil
	for key, want := range hp.flavor.likes {
		if have, ok := hp.likes[key]; !ok || have < want {
			hp.likes[key] = want
			hp.changed = append(hp.changed, key)
		}
	}
	slices.Sort(hp.changed)
}

// ---- report ------------------------------------------------------------------

func (p printer) history(hp *histPerson, taste tasteSource, loc *time.Location) {
	u := hp.user
	p.f("")
	p.f("@%s · %s · id %s", u.Username, u.Name, hp.uid())
	p.f("  Story: %s (%s), %s", hp.flavor.key, hp.flavor.about, hp.fitNote)
	if hp.backup != nil {
		p.f("  Originals: saved %s (kept; a rerun never overwrites them); dates hang off %s",
			hp.backup.SavedAt.In(loc).Format("Jan 2 3:04 PM"), hp.anchor.In(loc).Format("Mon Jan 2"))
	} else {
		p.f("  Originals: saved before the first change (%s)", strings.Join(historyFields, ", "))
	}
	rated := map[string]histRating{}
	for _, r := range hp.ratings {
		rated[r.itemID] = r
	}
	var c counts
	for _, pl := range hp.plans {
		c.add(pl.act)
	}
	p.f("  Past sidequests: %s", c)
	unrated := 0
	for _, pl := range hp.plans {
		it := pl.doc
		start, end := it.Start.In(loc), it.BackBy.In(loc)
		who := "solo"
		if len(pl.members) > 0 {
			who = "with " + strings.Join(pl.members, " and ")
		}
		line := fmt.Sprintf("    %s %s · %s, %s · %s", symbols[pl.act], it.Title, start.Format("Mon Jan 2"), httpx.TimeRange(start, end), who)
		if pl.note != "" {
			line += " (" + pl.note + ")"
		}
		p.f("%s", line)
		var stops []string
		for _, item := range it.Items {
			if item.Kind != models.ItemStop {
				continue
			}
			if r, ok := rated[item.ID]; ok {
				s := fmt.Sprintf("%s ★%d", item.Title, r.stars)
				if len(r.tags) > 0 {
					s += " “" + strings.Join(r.tags, "”, “") + "”"
				}
				if r.note != "" {
					s += " + note"
				}
				stops = append(stops, s)
			} else {
				stops = append(stops, item.Title+" (to rate)")
				unrated++
			}
		}
		p.f("        %s", strings.Join(stops, " → "))
	}
	p.f("  Ratings: %d rated through the app's rating handler, %d left to rate", len(hp.ratings), unrated)
	var diff []string
	for _, key := range contract.TripTypes {
		if !slices.Contains(hp.changed, key) {
			continue
		}
		old := "unset"
		if v, ok := hp.user.Prefs.Ratings[key]; ok {
			old = fmt.Sprint(v)
		}
		diff = append(diff, fmt.Sprintf("%s %s → %d", key, old, hp.likes[key]))
	}
	if len(diff) == 0 {
		diff = []string{"nothing to change (they already like all of it as much)"}
	}
	p.f("  Interests: %s", strings.Join(diff, ", "))
	p.f("  Taste vectors: %s", hp.taste)
	for _, n := range hp.notes {
		p.f("  Note: %s", n)
	}
}
