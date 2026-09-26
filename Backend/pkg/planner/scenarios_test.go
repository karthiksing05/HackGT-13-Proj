package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"strings"
	"testing"
	"time"
)

// The tuning scenarios: the demo user from Seaside Market Square on the
// Saltlight catalog, with the classifier scores the real ML service gave
// her for each request (classifier-v1 over local Qwen embeddings,
// 26 Sep 2026). Parks and hikes score high, bars, shopping and arcades
// low, and most events sit below the bar (Tau 0.5). -1: not scored in
// that scenario (another day, or out of range); the other columns stand in.
var realScores = map[string][4]float64{ // A, D, E, F
	"Anchor Park":                                         {0.85, 0.85, 0.82, 0.79},
	"Driftwood Forest Loop":                               {0.83, 0.85, 0.82, 0.79},
	"Old Rail Causeway":                                   {0.83, 0.83, 0.80, 0.79},
	"Heron Creek Greenway":                                {0.83, 0.82, 0.79, 0.77},
	"Pelican Island Tide Walk":                            {0.81, 0.80, 0.77, 0.75},
	"Kraken's Spine Traverse":                             {-1, -1, 0.81, -1},
	"Oracle Bluffs Ridge Trail":                           {-1, -1, 0.78, -1},
	"Sea Cliff Staircase Challenge":                       {-1, -1, 0.78, -1},
	"Coral Ridge Sunrise Trail":                           {0.78, 0.77, 0.74, 0.70},
	"Tidewater Dunes Trail":                               {-1, -1, 0.78, -1},
	"Kelp Hollow Marsh Boardwalk":                         {-1, -1, 0.77, -1},
	"Kelp Hollow Dog Beach":                               {0.77, 0.76, 0.74, 0.75},
	"Lantern Falls Trail":                                 {-1, -1, 0.77, -1},
	"Lighthouse Point Loop":                               {-1, -1, 0.76, -1},
	"Tidepool Discovery Beach":                            {0.76, 0.74, 0.70, 0.71},
	"Gull's Perch Overlook":                               {0.74, 0.76, 0.71, 0.63},
	"Mariner's Summit Trail":                              {0.75, 0.75, 0.75, 0.67},
	"Marina Community Garden":                             {-1, 0.72, 0.66, -1},
	"Mermaid Scoops":                                      {0.56, 0.72, 0.67, 0.58},
	"Shipwreck Cove Coastal Trail":                        {-1, -1, 0.70, -1},
	"Tidepool Walk with a Marine Biologist":               {-1, 0.70, 0.69, -1},
	"Sea Shanty Singalong at The Rusty Anchor":            {-1, -1, 0.39, 0.65},
	"Sunday Beach Volleyball Open Play":                   {-1, 0.56, 0.64, -1},
	"Harbor Ferry Loop":                                   {0.63, 0.64, 0.57, 0.54},
	"Harbor Kayak and SUP":                                {-1, 0.64, 0.59, 0.57},
	"Sweet Seaweed Vegan Kitchen":                         {0.46, 0.62, 0.54, 0.51},
	"Tidewater Botanical Gardens":                         {0.62, 0.59, 0.53, 0.54},
	"Saltmarsh Birding Loop":                              {-1, -1, 0.59, -1},
	"Brine and Bivalve Oyster Bar":                        {-1, 0.59, 0.57, 0.47},
	"Barnacle Street Mural Alley":                         {-1, -1, 0.59, -1},
	"Salt and Smoke BBQ Shack":                            {0.55, 0.56, 0.53, 0.52},
	"Seaside Market Hall":                                 {0.36, 0.54, 0.52, 0.37},
	"Sunset Jazz on Pier Nine":                            {0.54, -1, -1, -1},
	"Boardwalk Pickleball Round-Robin":                    {-1, -1, 0.51, -1},
	"Pho Real Noodle House":                               {0.39, 0.50, 0.45, 0.42},
	"The Rusty Anchor":                                    {0.50, 0.43, 0.41, 0.47},
	"Old Saltlight Lighthouse":                            {-1, -1, 0.50, -1},
	"Driftwood Coffee Roasters":                           {0.42, 0.49, 0.43, 0.47},
	"Poetry by the Tide":                                  {-1, -1, 0.48, -1},
	"Chess on the Boardwalk":                              {-1, 0.45, 0.41, -1},
	"Siren Song Lounge":                                   {0.31, -1, -1, 0.45},
	"Driftglass Gallery":                                  {0.41, 0.40, 0.39, 0.44},
	"Shipyard Robot Regatta":                              {-1, 0.40, 0.40, -1},
	"Lighthouse Ghost Walk":                               {-1, -1, -1, 0.40},
	"Sunday Makers' Swap Meet":                            {-1, 0.40, 0.38, -1},
	"Low Tide Taqueria":                                   {0.38, 0.35, 0.39, 0.33},
	"Pop the Balloon: Harbor Speed-Friending":             {-1, -1, -1, 0.36},
	"Bytes and Brine Cyber Cafe":                          {0.30, 0.32, 0.31, 0.33},
	"Salvage and Sons Vintage":                            {0.29, 0.32, 0.29, 0.30},
	"Paint the Lighthouse: Sip and Paint":                 {-1, -1, 0.30, -1},
	"Pixel Pier Arcade":                                   {0.22, 0.25, 0.30, 0.25},
	"Compass Rose Books and Maps":                         {0.27, 0.30, 0.25, 0.27},
	"Saltlight Mariners vs. Port Coral FC":                {-1, -1, 0.29, -1},
	"Shipwreck Escape Rooms":                              {-1, 0.22, 0.22, 0.28},
	"Fish Box Karaoke":                                    {0.21, -1, 0.16, 0.25},
	"The Crow's Nest Rooftop":                             {0.25, -1, 0.19, 0.22},
	"Lighthouse Laboratory Open House: VR Shipwreck Dive": {-1, -1, 0.22, -1},
	"AI for Good Roundtable":                              {-1, 0.22, 0.22, -1},
	"Saltlight Maritime Museum":                           {-1, 0.20, 0.16, -1},
	"Drag Brunch Ahoy!":                                   {-1, 0.18, -1, -1},
	"Oracle of the Deep Aquarium":                         {-1, 0.13, 0.13, 0.18},
	"Starboard Lanes":                                     {0.13, 0.16, 0.18, 0.18},
	"Kraken Climbing Gym":                                 {-1, 0.13, 0.13, 0.09},
	"The Lighthouse Laboratory":                           {-1, -1, 0.10, -1},
	"Circuit Cove Electronics":                            {0.00, -1, -1, -1},
}

// categoryScore stands in for anything the table lacks.
func categoryScore(a *models.Activity) float64 {
	switch a.Category {
	case "park", "hike":
		return 0.78
	case "viewpoint", "garden":
		return 0.65
	case "cafe", "restaurant", "market":
		return 0.45
	case "bar", "nightclub", "shopping", "rec_venue":
		return 0.25
	case "museum", "zoo_aquarium":
		return 0.15
	}
	return 0.35
}

// realScorer is the fake classifier for one scenario column.
func realScorer(col int) func(c *Candidate) float64 {
	return func(c *Candidate) float64 {
		v, ok := realScores[c.Act.Name]
		if !ok {
			return categoryScore(&c.Act)
		}
		if v[col] >= 0 {
			return v[col]
		}
		sum, n := 0.0, 0
		for _, x := range v {
			if x >= 0 {
				sum += x
				n++
			}
		}
		return sum / float64(n)
	}
}

type tuningScenario struct {
	name string
	col  int
	req  reqOpts
}

func tuningScenarios() []tuningScenario {
	base := func(from, backBy time.Time, rng, pace string, budget int, modes []string, mood string, tags ...string) reqOpts {
		return reqOpts{start: seasideMkt, end: seasideMkt, from: from, backBy: backBy, rng: rng, ride: "none",
			budget: budget, pace: pace, who: "just_me", modes: modes, mood: mood, tags: tags}
	}
	sun := func(h int) time.Time { return time.Date(2026, 9, 27, h, 0, 0, 0, ny) }
	walk := []string{"walk"}
	return []tuningScenario{
		{"A", 0, base(localAt(17, 30), localAt(21, 0), "walkable", "balanced", 1, walk, "something by the water, then music", "Outdoors", "Live music")},
		{"D", 1, base(sun(12), sun(16), "walkable", "balanced", 2, walk, "", "Outdoors", "Food")},
		{"E", 2, base(sun(13), sun(18), "transit", "packed", 2, []string{"marta", "walk"}, "", "Outdoors", "Food")},
		{"F", 3, base(sun(17), sun(21), "walkable", "relaxed", 2, walk, "", "Live music")},
	}
}

// TestTuningScenarios pins what the tuning pass promised on the demo
// catalog: fuller plans from drop-in stays and pace fit, never a stop
// below the bar while better plans exist, no travel-heavy options, and
// every option sound (§7) on every page.
func TestTuningScenarios(t *testing.T) {
	catalog := catalogByID(saltlight(t))
	tau := DefaultConfig().Itinerary.Tau
	for _, sc := range tuningScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			tp := newTestPlanner(saltlight(t), testConfig())
			tp.scorer.Fn = realScorer(sc.col)
			began := time.Now()
			batch, spec := tp.generate(t, sandy(), sc.req)
			if took := time.Since(began); !raceEnabled && took > 2*time.Second {
				t.Errorf("generate took %v", took)
			}
			if len(batch.Options) != 3 {
				t.Fatalf("first page: %d options (%s)", len(batch.Options), batch.Reason)
			}
			pool := tp.pool(t, batch.RunID)
			eff := effectiveSpec(t, spec, pool, batch.Relaxed)
			all := tp.allOptions(t, sandy(), batch)
			var counts []int
			for _, opt := range all {
				assertGuarantees(t, eff, opt, catalog)
				if opt.Metrics.TravelShare > DefaultConfig().MaxTravelShare {
					t.Errorf("%s travels %.0f%% of the time", opt.Name, 100*opt.Metrics.TravelShare)
				}
				for _, s := range opt.Stops {
					if score := realScorer(sc.col)(&Candidate{Act: catalog[s.ActivityID]}); score <= tau {
						t.Errorf("%s: %s scores %.2f, at or below the bar", opt.Name, s.Title, score)
					}
				}
				counts = append(counts, len(opt.Stops))
			}
			first := batch.Options
			t.Logf("stops per option %v; first page: %s | %s | %s", counts, first[0].Name, first[1].Name, first[2].Name)

			switch sc.name {
			case "A":
				// 17:30–21:00, balanced: two options of two stops or more,
				// one of them with the jazz set joined on time or a little
				// late and left early enough to walk home by 21:00.
				if n := multiStop(first, 2); n < 2 {
					t.Errorf("%d first-page options with 2+ stops, want 2", n)
				}
				jazz, ok := findStop(first, "Sunset Jazz on Pier Nine")
				if !ok {
					t.Fatal("no first-page option uses Sunset Jazz")
				}
				a := catalog[jazz.ActivityID]
				if jazz.Flexible || jazz.Arrive.Before(*a.Start) || jazz.Arrive.After(a.Start.Add(itinerary.LateArrival)) ||
					!jazz.Depart.Before(*a.End) || jazz.Depart.Sub(jazz.Arrive) < time.Hour {
					t.Errorf("jazz stay %v–%v (flexible %v) for a %v–%v set", jazz.Arrive.In(ny), jazz.Depart.In(ny), jazz.Flexible, a.Start.In(ny), a.End.In(ny))
				}
			case "D":
				// 12:00–16:00, balanced: 2–3 stops. The events that day score
				// below the bar for this user (0.18–0.45), except the tidepool
				// walk (0.70, 3.2 km: no walking leg reaches it) and the
				// volleyball (0.56, 2.8 km, only as a middle stop): the plans
				// are places, and the bar holds.
				if n := multiStop(first, 2); n != 3 {
					t.Errorf("%d first-page options with 2+ stops, want 3", n)
				}
			case "E":
				// 13:00–18:00, packed: 3–5 stops each.
				for _, opt := range first {
					if len(opt.Stops) < 3 || len(opt.Stops) > 5 {
						t.Errorf("%s: %d stops at a packed pace", opt.Name, len(opt.Stops))
					}
				}
			case "F":
				// 17:00–21:00, relaxed: the singalong starts at 17:00 and the
				// walk takes ten minutes, so the stay starts up to 15 late.
				s, ok := findStop(first, "Sea Shanty Singalong at The Rusty Anchor")
				if !ok {
					s, ok = findStop(first, "Pop the Balloon: Harbor Speed-Friending")
				}
				if !ok {
					t.Fatal("no first-page option with the singalong or the speed-friending")
				}
				a := catalog[s.ActivityID]
				if s.Flexible || s.Arrive.Before(*a.Start) || s.Arrive.After(a.Start.Add(itinerary.LateArrival)) {
					t.Errorf("%s joined %v for a %v start", s.Title, s.Arrive.In(ny), a.Start.In(ny))
				}
				for _, opt := range first {
					if len(opt.Stops) > 3 {
						t.Errorf("%s: %d stops at a relaxed pace", opt.Name, len(opt.Stops))
					}
				}
			}
		})
	}
}

func multiStop(opts []Option, min int) int {
	n := 0
	for _, o := range opts {
		if len(o.Stops) >= min {
			n++
		}
	}
	return n
}

func findStop(opts []Option, title string) (Stop, bool) {
	for _, o := range opts {
		for _, s := range o.Stops {
			if strings.EqualFold(s.Title, title) {
				return s, true
			}
		}
	}
	return Stop{}, false
}
