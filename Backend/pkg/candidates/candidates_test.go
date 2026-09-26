package candidates

import (
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var (
	ny, _ = time.LoadLocation("America/New_York")
	home  = travel.Point{Lat: 33.7766, Lng: -84.3890}
)

func at(h, m int) time.Time  { return time.Date(2026, 9, 26, h, m, 0, 0, ny) }
func f64(v float64) *float64 { return &v }

func near(dyKm float64) models.GeoJSONPoint {
	return models.GeoJSONPoint{Type: "Point", Coordinates: []float64{home.Lng, home.Lat + dyKm/111}}
}

func ev(name string, dyKm float64, start time.Time, end *time.Time) models.Activity {
	s := start
	return models.Activity{Kind: "event", Name: name, Category: "live_music", Location: near(dyKm), Start: &s, End: end}
}

func query() Query {
	return Query{
		Centers: []travel.Point{home}, RadiusKm: 3,
		From: at(17, 0), To: at(23, 30), Now: at(12, 0), MaxEventSpan: 6 * time.Hour,
		PlaceCategories: []string{"park", "bar", "hike"}, MinPlaceRating: 4,
	}
}

func TestKeep(t *testing.T) {
	endAt := func(h, m int) *time.Time { e := at(h, m); return &e }
	priced := func(a models.Activity, dollars float64) models.Activity {
		a.Price = &models.ActivityPrice{Min: f64(dollars)}
		return a
	}
	tagged := func(a models.Activity, tags ...string) models.Activity { a.Tags = tags; return a }
	place := func(cat string, rating *float64) models.Activity {
		return models.Activity{Kind: "place", Name: cat, Category: cat, Location: near(1), Rating: rating}
	}

	cases := []struct {
		name   string
		act    models.Activity
		tweak  func(*Query)
		reason string // "" = kept
	}{
		{"in window, nearby", ev("Show", 1, at(19, 0), nil), nil, ""},
		{"too far", ev("Show", 5, at(19, 0), nil), nil, RejectTooFar},
		{"near the end point", ev("Show", 5, at(19, 0), nil), func(q *Query) {
			q.Centers = append(q.Centers, travel.Point{Lat: home.Lat + 5/111.0, Lng: home.Lng})
		}, ""},
		{"no distance rule without centers", ev("Show", 50, at(19, 0), nil), func(q *Query) { q.Centers = nil }, ""},
		{"starts after back-by", ev("Late", 1, at(23, 45), nil), nil, RejectOutside},
		{"ended before the window", ev("Matinee", 1, at(13, 0), endAt(15, 0)), nil, RejectOutside},
		{"drop-in spanning the window", ev("Expo", 1, at(10, 0), endAt(20, 0)), nil, ""},
		{"no end, started 5 h before", ev("Fair", 1, at(12, 30), nil), nil, ""},
		{"no end, started 7 h before", ev("Fair", 1, at(10, 0), nil), nil, RejectOutside},
		{"already over", ev("Over", 1, at(17, 0), endAt(18, 0)), func(q *Query) { q.Now = at(19, 0) }, RejectOver},
		{"no start time", models.Activity{Kind: "event", Location: near(1)}, nil, RejectNoStart},
		{"no location", models.Activity{Kind: "event", Name: "x"}, nil, RejectNoLocation},
		{"over budget", priced(ev("Pricey", 1, at(19, 0), nil), 60), func(q *Query) { q.BudgetCents = 4000 }, RejectPrice},
		{"within budget", priced(ev("Cheap", 1, at(19, 0), nil), 25), func(q *Query) { q.BudgetCents = 4000 }, ""},
		{"no price is kept", ev("Unknown", 1, at(19, 0), nil), func(q *Query) { q.BudgetCents = 100 }, ""},
		{"avoided tag", tagged(ev("Rave", 1, at(19, 0), nil), "Crowded"), func(q *Query) { q.AvoidTags = []string{"crowded"} }, RejectAvoided},
		{"avoided category", ev("Gig", 1, at(19, 0), nil), func(q *Query) { q.AvoidTags = []string{"LIVE_MUSIC"} }, RejectAvoided},
		{"21+ for 18-20", ev("Club Night 21+", 1, at(19, 0), nil), func(q *Query) { q.AgeBracket = "18_20" }, RejectAge},
		{"18+ for 18-20", ev("Club Night 18+", 1, at(19, 0), nil), func(q *Query) { q.AgeBracket = "18_20" }, ""},
		{"18+ for 13-17", ev("Club Night 18+", 1, at(19, 0), nil), func(q *Query) { q.AgeBracket = "13_17" }, RejectAge},
		{"good bar", place("bar", f64(4.5)), nil, ""},
		{"poor bar", place("bar", f64(3.2)), nil, RejectRating},
		{"unrated hike", place("hike", nil), nil, ""},
		{"unrated park", place("park", nil), nil, RejectRating},
		{"venue-type place", place("cinema", f64(4.8)), nil, RejectCategory},
	}
	for _, c := range cases {
		q := query()
		if c.tweak != nil {
			c.tweak(&q)
		}
		a := c.act
		ok, reason := q.Keep(&a)
		if c.reason == "" && !ok {
			t.Errorf("%s: rejected (%s), want kept", c.name, reason)
		}
		if c.reason != "" && reason != c.reason {
			t.Errorf("%s: got %q, want %q", c.name, reason, c.reason)
		}
	}
}

func TestMongoFiltersCoverEveryRule(t *testing.T) {
	q := query()
	q.BudgetCents = 4000
	q.AvoidTags = []string{"crowded"}
	q.AgeBracket = "13_17"
	q.Centers = append(q.Centers, travel.Point{Lat: 33.8, Lng: -84.4})
	ev := q.EventFilter()["$and"]
	pl := q.PlaceFilter()["$and"]
	// kind, start, overlap, geo, budget, category+tags avoid, age
	if n := len(ev.(bson.A)); n != 8 {
		t.Errorf("event filter has %d clauses, want 8", n)
	}
	// kind, category, rating, geo, budget, avoid x2, age
	if n := len(pl.(bson.A)); n != 8 {
		t.Errorf("place filter has %d clauses, want 8", n)
	}
}
