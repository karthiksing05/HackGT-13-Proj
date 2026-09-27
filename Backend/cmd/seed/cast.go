package main

import (
	"Backend/pkg/contract"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// The two worlds (store.CatalogFor decides who sees which): Atlanta is what
// every regular account sees, Saltlight what the demo account sees.
const (
	worldAtlanta   = "atlanta"
	worldSaltlight = "saltlight"
)

// defaultDemoEmail is Sandy Byte's address (docs/DEMO_ACCOUNT.md).
const defaultDemoEmail = "demo@sidequestz.tech"

// sandyKey names the demo account in the Saltlight specs; she is looked up,
// never written.
const sandyKey = "sandy"

// person is one showcase account. The seed creates it under id unless an
// account behind one of adoptEmails already exists in the demo cast (Marin
// and Theo predate this seed): that one is used as it is and never changed.
type person struct {
	key         string
	id          bson.ObjectID
	name        string
	username    string
	email       string
	adoptEmails []string
	school      string
	avatar      contract.AvatarColor
	status      contract.PresenceStatus
	birth       time.Time
	prefs       contract.Preferences
}

// pair is a friendship between two people of a world (cast keys, or sandyKey).
type pair struct {
	a, b string
	days int // friends for this many days
}

// requestSpec is a pending friend request.
type requestSpec struct {
	from, to, note string
	ago            time.Duration
}

// stopSpec is one stop of a plan: the first catalog place of names that is
// open then, else the nearest open place of categories, else the nearest
// open place of any kind.
type stopSpec struct {
	names      []string
	categories []string
	minutes    int
}

// chatLine is one group-chat message. Text may use {s1}…{s3} (the stops'
// names), {start} ("5:30 PM") and {when} ("tonight", "tomorrow", …).
type chatLine struct {
	from string
	ago  time.Duration
	text string
}

// planSpec is one shared sidequest with its group chat. It starts day days
// after the world's today at start (local), or a day later when that is
// less than leadTime away.
type planSpec struct {
	key        string
	title      string
	mood       string
	host       string
	members    []string
	visibility string // open | friends
	maxSize    int    // 0 = no limit
	day        int
	start      [2]int // hour, minute
	anchor     travel.Point
	stops      []stopSpec
	createdAgo time.Duration
	chat       []chatLine
	unread     map[string]int // member key → unread messages in the chat
}

// postSpec is a "free now" Forum post in an area. The area's point is the
// catalog place place when the catalog has it, else point.
type postSpec struct {
	author     string
	area       string
	place      string
	point      travel.Point
	visibility string // everyone | friends
	untilHour  int    // free until this hour tonight (or two hours from now if that is sooner)
	ago        time.Duration
}

// worldSpec is everything one world holds.
type worldSpec struct {
	name    string
	catalog string
	city    string
	roles   []string
	about   string
	// center is where the Forum looks by default (the app's Midtown area, or
	// Sandy's home base); a plan's first stop is at most maxFromCenter miles
	// from it, so the default feed shows every plan.
	center        travel.Point
	maxFromCenter float64
	cast          []person
	friendships   []pair
	requests      []requestSpec
	plans         []planSpec
	posts         []postSpec
}

func oid(hex string) bson.ObjectID {
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		panic("seed: " + err.Error())
	}
	return id
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// prefs builds validated preferences (the contract's shape; Validate fills
// nothing here because every enum is given).
func prefs(ratings map[string]int, company contract.Company, pace contract.Pace, spend contract.SpendTier,
	flex contract.Flexibility, split contract.SplitStyle, preferFree bool, perfect, never, around string) contract.Preferences {
	p := contract.Preferences{
		Ratings: contract.Ratings(ratings), Company: company, Pace: pace, Spend: spend, Flexibility: flex,
		SplitStyle: split, PreferFree: preferFree, InstantCheckoutLimitCents: 5000,
		Answers: contract.Answers{"perfect_afternoon": perfect, "never_do": never, "plan_around": around},
	}
	if err := p.Validate(); err != nil {
		panic("seed: preferences: " + err.Error())
	}
	return p
}

// Places near Midtown Atlanta (the app's default Forum area) and in Saltlight.
var (
	midtown         = travel.Point{Lat: 33.7838, Lng: -84.3833} // ForumArea.midtown
	techSquare      = travel.Point{Lat: 33.7767, Lng: -84.3895}
	gtCampus        = travel.Point{Lat: 33.7756, Lng: -84.3963} // the app's "Georgia Tech campus" area
	piedmontPark    = travel.Point{Lat: 33.7879, Lng: -84.3722}
	ponceCityMarket = travel.Point{Lat: 33.7728, Lng: -84.3656}
	westsideAnchor  = travel.Point{Lat: 33.7814, Lng: -84.4139}
	seasideSquare   = travel.Point{Lat: 31.3680, Lng: -81.4250} // Sandy's home base
)

func atlantaPerson(key, hex, name, handle, school string, avatar contract.AvatarColor, status contract.PresenceStatus,
	birth time.Time, p contract.Preferences) person {
	return person{key: key, id: oid(hex), name: name, username: handle, email: handle + "@showcase.sidequestz.tech",
		school: school, avatar: avatar, status: status, birth: birth, prefs: p}
}

// atlanta is the world of every account outside the demo cast: people with
// the role "showcase" (never demo or bot, so they share the pitch catalog,
// People for you and the Forum with real sign-ups), five shared plans on
// today's and tomorrow's evenings around Midtown, Tech Square and the
// BeltLine, and three people free right now near Tech Square.
var atlanta = worldSpec{
	name: worldAtlanta, catalog: store.DefaultCatalog, city: "atlanta", roles: []string{"showcase"},
	about:  "what every regular account sees",
	center: midtown, maxFromCenter: 1.9,
	cast: []person{
		atlantaPerson("amara", "5eed5c00000000000000a001", "Amara Okonkwo", "amara.okonkwo", "Spelman College",
			contract.AvatarClay, contract.StatusOpen, day(2004, time.February, 11),
			prefs(map[string]int{"live_music": 5, "food": 5, "museums": 4, "nightlife": 3, "shopping": 3, "outdoors": 3,
				"big_crowds": 3, "long_walks": 2, "sports": 2, "early_mornings": 2},
				contract.CompanySmallGroup, contract.PaceBalanced, contract.Spend15To40, contract.FlexBitOverOK, contract.SplitEqually, false,
				"Brunch with my roommates, a gallery or two, then a live set somewhere with good sound.",
				"Anything that starts before 9 AM on a weekend.",
				"Friday shows and Sunday brunch.")),
		atlantaPerson("diego", "5eed5c00000000000000a002", "Diego Ramos", "diego.ramos", "Georgia Tech",
			contract.AvatarInk, contract.StatusFriendsOnly, day(2003, time.November, 3),
			prefs(map[string]int{"outdoors": 5, "long_walks": 5, "sports": 4, "food": 4, "early_mornings": 4, "live_music": 3,
				"museums": 2, "nightlife": 2, "big_crowds": 2, "shopping": 1},
				contract.CompanySmallGroup, contract.PacePacked, contract.SpendUnder15, contract.FlexStickToBudget, contract.SplitPayOwn, true,
				"A long BeltLine loop, tacos at the end, and a sunset from somewhere high up.",
				"Waiting in a line for anything.",
				"Weekend runs and whoever wants to explore.")),
		atlantaPerson("hana", "5eed5c00000000000000a003", "Hana Kim", "hana.kim", "SCAD Atlanta",
			contract.AvatarSage, contract.StatusOpen, day(2004, time.June, 22),
			prefs(map[string]int{"museums": 5, "shopping": 4, "food": 4, "live_music": 4, "outdoors": 3, "long_walks": 3,
				"early_mornings": 3, "nightlife": 2, "sports": 1, "big_crowds": 1},
				contract.CompanySmallGroup, contract.PaceRelaxed, contract.SpendUnder15, contract.FlexBitOverOK, contract.SplitEqually, true,
				"Sketching in a quiet museum, then coffee and a record store.",
				"Loud clubs, or anything with a two-drink minimum.",
				"Gallery openings and thrift runs.")),
		atlantaPerson("marcus", "5eed5c00000000000000a004", "Marcus Bell", "marcus.bell", "Morehouse College",
			contract.AvatarForest, contract.StatusOpen, day(2003, time.August, 30),
			prefs(map[string]int{"sports": 5, "live_music": 4, "food": 4, "nightlife": 4, "big_crowds": 4, "outdoors": 3,
				"museums": 2, "shopping": 2, "long_walks": 2, "early_mornings": 1},
				contract.CompanyBigGroup, contract.PacePacked, contract.Spend15To40, contract.FlexBitOverOK, contract.SplitTakeTurns, false,
				"Pickup basketball, wings with the crew, then the game on a big screen.",
				"Sitting still for three hours.",
				"Game days and whoever's hosting.")),
		atlantaPerson("sofia", "5eed5c00000000000000a005", "Sofia Mendes", "sofia.mendes", "Emory",
			contract.AvatarSand, contract.StatusOpen, day(2004, time.January, 17),
			prefs(map[string]int{"outdoors": 5, "early_mornings": 5, "food": 4, "long_walks": 4, "museums": 4, "live_music": 3,
				"shopping": 3, "sports": 3, "nightlife": 1, "big_crowds": 1},
				contract.CompanySmallGroup, contract.PaceBalanced, contract.SpendUnder15, contract.FlexStickToBudget, contract.SplitEqually, true,
				"A picnic in Piedmont Park, a long walk, and the sunset from a rooftop.",
				"Crowded bars on a school night.",
				"Sunrise runs and farmers markets.")),
		atlantaPerson("jalen", "5eed5c00000000000000a006", "Jalen Brooks", "jalen.brooks", "Georgia State",
			contract.AvatarInk, contract.StatusBusy, day(2003, time.May, 9),
			prefs(map[string]int{"nightlife": 5, "live_music": 5, "big_crowds": 4, "sports": 4, "food": 3, "shopping": 3,
				"outdoors": 2, "museums": 2, "long_walks": 1, "early_mornings": 1},
				contract.CompanyBigGroup, contract.PacePacked, contract.Spend15To40, contract.FlexBitOverOK, contract.SplitPayOwn, false,
				"A day party, a nap, then a show that runs late.",
				"Early mornings. Any of them.",
				"Concerts and my friends' DJ sets.")),
		atlantaPerson("lily", "5eed5c00000000000000a007", "Lily Tran", "lily.tran", "Georgia Tech",
			contract.AvatarClay, contract.StatusOpen, day(2005, time.March, 28),
			prefs(map[string]int{"food": 5, "shopping": 4, "museums": 4, "outdoors": 4, "long_walks": 4, "live_music": 3,
				"sports": 2, "nightlife": 2, "big_crowds": 2, "early_mornings": 2},
				contract.CompanySmallGroup, contract.PaceBalanced, contract.SpendUnder15, contract.FlexBitOverOK, contract.SplitEqually, true,
				"Trying a new food hall stall, walking it off, then games with friends.",
				"Anything that needs a reservation three weeks out.",
				"Study breaks and every noodle place in town.")),
		atlantaPerson("omar", "5eed5c00000000000000a008", "Omar Haddad", "omar.haddad", "Emory",
			contract.AvatarSage, contract.StatusOpen, day(2004, time.October, 14),
			prefs(map[string]int{"live_music": 5, "outdoors": 4, "food": 4, "museums": 3, "nightlife": 3, "long_walks": 3,
				"sports": 3, "early_mornings": 3, "shopping": 2, "big_crowds": 2},
				contract.CompanySmallGroup, contract.PaceBalanced, contract.SpendUnder15, contract.FlexBitOverOK, contract.SplitEqually, true,
				"Coffee, a bike ride on the BeltLine, then a small show at night.",
				"Tourist traps with long lines.",
				"Open mics and weekend bike rides.")),
	},
	friendships: []pair{
		{"diego", "sofia", 210}, {"diego", "lily", 95}, {"diego", "omar", 40}, {"diego", "marcus", 12},
		{"sofia", "hana", 150}, {"sofia", "lily", 60}, {"hana", "amara", 180}, {"hana", "omar", 25},
		{"amara", "marcus", 320}, {"amara", "jalen", 75}, {"marcus", "jalen", 400}, {"lily", "omar", 8},
	},
	plans: []planSpec{
		{
			key: "beltline", title: "Sunset on the BeltLine", mood: "golden hour walk, then a patio",
			host: "diego", members: []string{"sofia"}, visibility: "open", maxSize: 6, day: 0, start: [2]int{17, 30},
			anchor: piedmontPark, createdAgo: 5 * time.Hour,
			stops: []stopSpec{
				{names: []string{"Piedmont Park"}, categories: []string{"park", "garden", "hike"}, minutes: 45},
				{names: []string{"Historic Fourth Ward Park"}, categories: []string{"park", "hike"}, minutes: 30},
				{names: []string{"New Realm Brewing Co."}, categories: []string{"restaurant", "bar"}, minutes: 75},
			},
			chat: []chatLine{
				{"diego", 4*time.Hour + 10*time.Minute, "Walking the BeltLine at golden hour {when}. Meet at {s1} at {start}?"},
				{"sofia", 3*time.Hour + 55*time.Minute, "In. I'll bring the speaker if you bring snacks"},
				{"diego", 3*time.Hour + 20*time.Minute, "Deal. {s2} after, then food at {s3}"},
				{"sofia", 50 * time.Minute, "Might be 5 late, start without me and I'll catch up"},
			},
		},
		{
			key: "rooftop", title: "Food hall dinner, then rooftop games", mood: "cheap eats and skee-ball",
			host: "lily", members: []string{"omar"}, visibility: "open", maxSize: 5, day: 0, start: [2]int{18, 0},
			anchor: ponceCityMarket, createdAgo: 7 * time.Hour,
			stops: []stopSpec{
				{names: []string{"Ponce City Market"}, categories: []string{"market", "restaurant"}, minutes: 60},
				{names: []string{"Skyline Park"}, categories: []string{"rec_venue"}, minutes: 90},
			},
			chat: []chatLine{
				{"lily", 6 * time.Hour, "Study break {when}! Dinner at {s1}, then games at {s2}"},
				{"omar", 5*time.Hour + 40*time.Minute, "Loser of the first game buys dessert"},
				{"lily", 2*time.Hour + 5*time.Minute, "You're on. Meet by the main entrance at {start}"},
			},
		},
		{
			key: "picnic", title: "Picnic in Piedmont Park", mood: "blankets, snacks and the skyline",
			host: "sofia", members: []string{"hana", "diego"}, visibility: "friends", day: 1, start: [2]int{17, 30},
			anchor: piedmontPark, createdAgo: 26 * time.Hour,
			stops: []stopSpec{
				{names: []string{"Piedmont Park"}, categories: []string{"park", "garden"}, minutes: 90},
				{names: []string{"Sugar Factory American Brasserie", "Eleventh Street Pub"}, categories: []string{"restaurant"}, minutes: 75},
			},
			chat: []chatLine{
				{"sofia", 25 * time.Hour, "Picnic {when} at {start}! I'll grab a spot by the lake"},
				{"hana", 24*time.Hour + 30*time.Minute, "Bringing my sketchbook and too many clementines"},
				{"diego", 3 * time.Hour, "Count me in. Dinner at {s2} after?"},
				{"sofia", 2*time.Hour + 40*time.Minute, "Yes please"},
			},
		},
		{
			key: "duckpin", title: "Duckpin bowling on the Westside", mood: "bowling, then blues",
			host: "marcus", members: []string{"jalen", "amara"}, visibility: "open", maxSize: 8, day: 1, start: [2]int{19, 0},
			anchor: westsideAnchor, createdAgo: 10 * time.Hour,
			stops: []stopSpec{
				{names: []string{"The Painted Duck"}, categories: []string{"rec_venue"}, minutes: 90},
				{names: []string{"Northside Tavern"}, categories: []string{"bar", "restaurant"}, minutes: 75},
			},
			chat: []chatLine{
				{"marcus", 9 * time.Hour, "Lanes at {s1} {when} at {start}. Anyone can join, we'll grab a second lane"},
				{"jalen", 8*time.Hour + 30*time.Minute, "I'm there after work"},
				{"amara", 7 * time.Hour, "Blues at {s2} after? I'm in"},
				{"marcus", 90 * time.Minute, "Obviously"},
			},
		},
		{
			key: "crawl", title: "Midtown dinner crawl", mood: "three stops, all walkable",
			host: "omar", members: []string{"lily"}, visibility: "open", maxSize: 6, day: 1, start: [2]int{19, 30},
			anchor: techSquare, createdAgo: 6 * time.Hour,
			stops: []stopSpec{
				{names: []string{"Cypress Street Pint & Plate"}, categories: []string{"restaurant"}, minutes: 60},
				{names: []string{"Fado Irish Pub"}, categories: []string{"restaurant", "bar"}, minutes: 60},
				{names: []string{"Eleventh Street Pub"}, categories: []string{"restaurant", "bar"}, minutes: 60},
			},
			chat: []chatLine{
				{"omar", 5 * time.Hour, "Three stops from Tech Square, all walking. First round at {s1} at {start}"},
				{"lily", 4*time.Hour + 40*time.Minute, "I'll meet you at {s2}, lab runs late"},
				{"omar", 35 * time.Minute, "Saving you a seat"},
			},
		},
	},
	posts: []postSpec{
		{author: "hana", area: "Tech Square", point: techSquare, visibility: "everyone", untilHour: 21, ago: 25 * time.Minute},
		{author: "amara", area: "Georgia Tech campus", point: gtCampus, visibility: "everyone", untilHour: 22, ago: 8 * time.Minute},
		{author: "marcus", area: "Midtown", point: midtown, visibility: "everyone", untilHour: 23, ago: 50 * time.Minute},
	},
}

func saltlightBot(key, hex, name, handle, school string, avatar contract.AvatarColor, birth time.Time,
	p contract.Preferences, adopt ...string) person {
	email := key + "@bots.sidequestz.tech"
	return person{key: key, id: oid(hex), name: name, username: handle, email: email, adoptEmails: append([]string{email}, adopt...),
		school: school, avatar: avatar, status: contract.StatusOpen, birth: birth, prefs: p}
}

// saltlight is the demo account's world (catalog demo_activities, on the
// demo date): bots with the role "bot", four of them Sandy's friends and one
// asking to be, three open plans near Seaside Market Square, a group plan
// Sandy is in, and two bots free right now.
var saltlight = worldSpec{
	name: worldSaltlight, catalog: store.CollDemoActivities, city: "saltlight", roles: []string{"bot"},
	about:  "what the demo account sees",
	center: seasideSquare, maxFromCenter: 1.9,
	cast: []person{
		saltlightBot("marin", "5eed5c00000000000000b001", "Marin Okafor", "marinokafor", "", contract.AvatarClay, day(2002, time.April, 19),
			prefs(map[string]int{"food": 5, "outdoors": 4, "live_music": 4, "shopping": 4, "long_walks": 3, "museums": 3,
				"nightlife": 2, "sports": 2, "big_crowds": 2, "early_mornings": 3},
				contract.CompanySmallGroup, contract.PaceBalanced, contract.SpendUnder15, contract.FlexBitOverOK, contract.SplitEqually, true,
				"The Saturday market, a long lunch and golden hour by the water.",
				"Anything indoors when the sun is out.",
				"Market mornings and sunset walks."), "marin@gatech.edu"),
		saltlightBot("theo", "5eed5c00000000000000b002", "Theo Park", "theopark", "", contract.AvatarForest, day(2003, time.January, 25),
			prefs(map[string]int{"live_music": 5, "nightlife": 4, "food": 4, "outdoors": 3, "sports": 3, "museums": 2,
				"shopping": 2, "long_walks": 2, "big_crowds": 3, "early_mornings": 1},
				contract.CompanySmallGroup, contract.PaceBalanced, contract.SpendUnder15, contract.FlexBitOverOK, contract.SplitEqually, false,
				"Tacos, a pub quiz and a singalong that goes too long.",
				"Early ferries.",
				"Open mics and trivia nights."), "theo@gatech.edu"),
		saltlightBot("juno", "5eed5c00000000000000b003", "Juno Reyes", "junoreyes", "Saltlight Harbor College", contract.AvatarSage,
			day(2003, time.September, 2),
			prefs(map[string]int{"sports": 5, "nightlife": 4, "food": 4, "outdoors": 4, "live_music": 3, "big_crowds": 3,
				"museums": 2, "shopping": 2, "long_walks": 3, "early_mornings": 2},
				contract.CompanySmallGroup, contract.PacePacked, contract.SpendUnder15, contract.FlexBitOverOK, contract.SplitTakeTurns, false,
				"Climbing in the morning, bowling at night, tacos in between.",
				"Sitting through a three-hour dinner.",
				"League nights and anything competitive.")),
		saltlightBot("kai", "5eed5c00000000000000b004", "Kai Nakamura", "kainakamura", "Saltlight Harbor College", contract.AvatarInk,
			day(2002, time.December, 15),
			prefs(map[string]int{"outdoors": 5, "long_walks": 4, "food": 4, "museums": 4, "live_music": 3, "early_mornings": 4,
				"sports": 3, "shopping": 2, "nightlife": 2, "big_crowds": 1},
				contract.CompanySmallGroup, contract.PaceRelaxed, contract.SpendUnder15, contract.FlexStickToBudget, contract.SplitEqually, true,
				"The ferry loop, oysters at happy hour and a slow walk home along the harbor.",
				"Packed clubs.",
				"Ferry schedules and tide tables.")),
		saltlightBot("rosa", "5eed5c00000000000000b005", "Rosa Delgado", "rosadelgado", "", contract.AvatarSand, day(2004, time.July, 7),
			prefs(map[string]int{"live_music": 5, "nightlife": 4, "museums": 4, "food": 3, "shopping": 4, "outdoors": 3,
				"big_crowds": 3, "sports": 2, "long_walks": 2, "early_mornings": 1},
				contract.CompanyBigGroup, contract.PaceBalanced, contract.Spend15To40, contract.FlexBitOverOK, contract.SplitEqually, false,
				"An escape room with friends, then karaoke until they kick us out.",
				"Quiet nights in.",
				"Karaoke Thursdays."))},
	friendships: []pair{
		{sandyKey, "marin", 30}, {sandyKey, "theo", 21}, {sandyKey, "juno", 45}, {sandyKey, "kai", 9},
		{"marin", "theo", 200}, {"marin", "juno", 120}, {"juno", "kai", 300}, {"kai", "theo", 60}, {"kai", "rosa", 90},
	},
	requests: []requestSpec{
		{from: "rosa", to: sandyKey, note: "Kai says you're the one to ask about karaoke", ago: 2 * time.Hour},
	},
	plans: []planSpec{
		{
			key: "shanties", title: "Tacos, then sea shanties", mood: "fish tacos and loud singing",
			host: "theo", members: []string{"marin"}, visibility: "open", maxSize: 6, day: 0, start: [2]int{18, 30},
			anchor: seasideSquare, createdAgo: 4 * time.Hour,
			stops: []stopSpec{
				{names: []string{"Low Tide Taqueria"}, categories: []string{"restaurant"}, minutes: 60},
				{names: []string{"The Rusty Anchor"}, categories: []string{"bar"}, minutes: 90},
			},
			chat: []chatLine{
				{"theo", 3 * time.Hour, "Tacos at {s1} {when}, then shanties at {s2}. Who's in?"},
				{"marin", 2*time.Hour + 45*time.Minute, "I know exactly one shanty. It will be enough"},
			},
		},
		{
			key: "ferry", title: "Golden hour ferry and oysters", mood: "ferry loop at sunset, then oysters",
			host: "kai", members: []string{"theo"}, visibility: "open", maxSize: 8, day: 1, start: [2]int{17, 0},
			anchor: seasideSquare, createdAgo: 6 * time.Hour,
			stops: []stopSpec{
				{names: []string{"Harbor Ferry Loop"}, categories: []string{"tour"}, minutes: 60},
				{names: []string{"Brine and Bivalve Oyster Bar"}, categories: []string{"restaurant"}, minutes: 75},
			},
			chat: []chatLine{
				{"kai", 5 * time.Hour, "The ferry leaves on the hour. Meet at {s1} at {start} {when}"},
				{"theo", 4*time.Hour + 20*time.Minute, "I'm in for the ferry. Oysters are a maybe"},
			},
		},
		{
			key: "escape", title: "Escape room, then karaoke", mood: "puzzles, then singing it off",
			host: "rosa", members: []string{"marin"}, visibility: "open", maxSize: 5, day: 1, start: [2]int{17, 30},
			anchor: seasideSquare, createdAgo: 8 * time.Hour,
			stops: []stopSpec{
				{names: []string{"Shipwreck Escape Rooms"}, categories: []string{"rec_venue"}, minutes: 90},
				{names: []string{"Fish Box Karaoke"}, categories: []string{"rec_venue", "bar"}, minutes: 90},
			},
			chat: []chatLine{
				{"rosa", 7 * time.Hour, "Escape room at {s1} {when} at {start}, then karaoke at {s2}. A few spots left"},
				{"marin", 6*time.Hour + 30*time.Minute, "Calling the first song now"},
			},
		},
		{
			key: "bowling", title: "Bowling night", mood: "glow bowling, then the rooftop",
			host: "juno", members: []string{sandyKey, "kai"}, visibility: "friends", day: 1, start: [2]int{19, 30},
			anchor: seasideSquare, createdAgo: 9 * time.Hour,
			stops: []stopSpec{
				{names: []string{"Starboard Lanes"}, categories: []string{"rec_venue"}, minutes: 90},
				{names: []string{"The Crow's Nest Rooftop"}, categories: []string{"bar"}, minutes: 60},
			},
			chat: []chatLine{
				{"juno", 8 * time.Hour, "Lane's booked at {s1} {when} at {start}. Sandy, you're on my team"},
				{"kai", 7*time.Hour + 40*time.Minute, "Winner picks the table at {s2}"},
				{"juno", 40 * time.Minute, "Shoes are on me this time"},
			},
			unread: map[string]int{sandyKey: 2},
		},
	},
	posts: []postSpec{
		{author: "juno", area: "Seaside Market", place: "Seaside Market Hall", point: seasideSquare, visibility: "friends",
			untilHour: 21, ago: 15 * time.Minute},
		{author: "kai", area: "The Shipyard", place: "The Shipyard Makerspace", point: seasideSquare, visibility: "everyone",
			untilHour: 22, ago: 35 * time.Minute},
	},
}

// worlds are the known worlds by name, in the order they run.
var worlds = []*worldSpec{&atlanta, &saltlight}
