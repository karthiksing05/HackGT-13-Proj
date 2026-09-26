package planner

import (
	"Backend/pkg/itinerary"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const kmPerMile = 1.609344

// render turns the run's best plans into options, "Best match" first.
// Every option is checked against the guarantees before it is shown; one
// that fails would mean a bug upstream, so it is left out and logged.
func (p *Planner) render(run *Run) []Option {
	lookup := func(id string) (*models.Activity, bool) {
		if c := run.Pool.Get(id); c != nil {
			return &c.Act, true
		}
		return nil, false
	}
	out := make([]Option, 0, len(run.Best))
	used := map[string]bool{}
	for _, sp := range run.Best {
		opt := renderOption(run, sp, len(out))
		if v := CheckOption(&run.Spec, &opt, lookup); len(v) > 0 {
			run.rejected++
			log.Error().Str("run", run.ID).Str("signature", sp.Signature).Strs("violations", v).
				Msg("planner: option failed the guarantees; left out")
			continue
		}
		opt.Tag = optionTag(len(out), opt, used, run.Spec)
		used[opt.Tag] = true
		out = append(out, opt)
	}
	return out
}

func renderOption(run *Run, sp ScoredPlan, n int) Option {
	it := sp.It
	spec := &run.Spec
	opt := Option{
		ID:               OptionID(run.ID, n),
		Score:            round5(sp.Score),
		Metrics:          sp.Metrics,
		Depart:           it.Depart.UTC(),
		Arrival:          it.Arrival.UTC(),
		LateFlag:         it.LateRisk,
		TotalDurationMin: int(it.Arrival.Sub(it.Depart).Minutes()),
		CostKnown:        true,
	}
	for i, s := range it.Stops {
		stop := renderStop(run, s, i)
		if !stop.PriceKnown {
			opt.CostKnown = false
		} else {
			opt.TotalCostCents += *stop.PriceCents
		}
		opt.Stops = append(opt.Stops, stop)
	}
	totalKm := 0.0
	var summaries []string
	for i, leg := range it.Legs {
		l := renderLeg(leg, i, opt.Stops, spec.DriveLabel)
		totalKm += leg.DistanceKm
		summaries = append(summaries, fmt.Sprintf("%s %d min", legVerb(l.Mode), l.Minutes))
		opt.Legs = append(opt.Legs, l)
	}
	opt.RouteSummary = strings.Join(summaries, " → ")
	opt.Name = optionName(opt.Stops)
	opt.Summary = optionSummary(opt, spec, totalKm)
	opt.Meta = optionMeta(opt, spec, totalKm)
	return opt
}

func renderStop(run *Run, s itinerary.Stop, i int) Stop {
	node := s.Node
	a := node.Act
	stop := stopFromActivity(a, run.Spec.TZ)
	stop.ID = StopID(a.ID.Hex(), i)
	stop.Order = i
	stop.Arrive, stop.Depart = node.Start.UTC(), node.End.UTC()
	stop.DurationMinutes = int(node.End.Sub(node.Start).Minutes())
	stop.Flexible = node.Flexible
	stop.SeriesKey = node.SeriesKey
	if node.Flexible {
		stop.OpenSlots = openSlotsFor(a, run.Spec.TZ, run.Spec.From, run.Spec.BackBy)
	}
	if c := run.Pool.Get(a.ID.Hex()); c != nil {
		stop.Utility = round5(c.Raw())
	}
	stop.Subtitle = stopSubtitle(stop, a, run.Spec.TZ)
	return stop
}

// stopFromActivity fills the activity-level fields of a stop (no times).
func stopFromActivity(a *models.Activity, tz *time.Location) Stop {
	stop := Stop{
		Title:      strings.TrimSpace(a.Name),
		ActivityID: a.ID.Hex(),
		Kind:       a.Kind,
		Category:   a.Category,
		Tags:       append([]string(nil), a.Tags...),
		Address:    activityAddress(a),
		WebsiteURL: strValue(a.URL),
		TicketURL:  strValue(a.TicketURL),
		ImageURL:   strValue(a.ImageURL),
		Summary:    strValue(a.Summary),
	}
	if stop.Tags == nil {
		stop.Tags = []string{}
	}
	if pt, ok := activityPoint(a); ok {
		stop.Place = PlaceAt(placeName(a), pt)
	} else {
		stop.Place = Place{Name: placeName(a)}
	}
	if cents, known := activityCost(a); known {
		stop.PriceCents, stop.PriceKnown = &cents, true
	}
	stop.Tier, stop.TierKnown = activityTier(a)
	stop.DurationMinutes = 60
	if a.Duration != nil && a.Duration.MedianMin > 0 && !math.IsInf(a.Duration.MedianMin, 0) {
		stop.DurationMinutes = int(math.Min(a.Duration.MedianMin, 6*60))
	}
	stop.Subtitle = stopSubtitle(stop, a, tz)
	return stop
}

// openSlotPad widens the stored opening slots beyond the plan's window, so
// a re-route with a moved start or back-by time can still be checked.
const openSlotPad = 12 * time.Hour

// openSlotsFor is when a flexible activity can be visited around the
// window: a place's opening hours, a drop-in event's span.
func openSlotsFor(a *models.Activity, tz *time.Location, from, to time.Time) []TimeSlot {
	from, to = from.Add(-openSlotPad), to.Add(openSlotPad)
	if a.Kind == "place" {
		loc := tz
		if a.Timezone != "" {
			if l, err := time.LoadLocation(a.Timezone); err == nil {
				loc = l
			}
		}
		if loc == nil {
			return nil
		}
		ivs, ok := itinerary.OpenIntervals(a.WeeklyHours, a.Category, loc, from, to)
		if !ok {
			return nil
		}
		out := make([]TimeSlot, len(ivs))
		for i, iv := range ivs {
			out[i] = TimeSlot{From: iv.Start.UTC(), To: iv.End.UTC()}
		}
		return out
	}
	if a.Start == nil {
		return nil
	}
	start := a.Start.UTC()
	end := to
	if a.End != nil && a.End.After(start) {
		end = a.End.UTC()
	} else {
		_, median := visitLengths(a)
		end = start.Add(median)
	}
	return []TimeSlot{{From: start, To: end}}
}

// withinSlots reports whether [start, end] fits inside one slot.
func withinSlots(start, end time.Time, slots []TimeSlot) bool {
	for _, s := range slots {
		if !start.Before(s.From) && !end.After(s.To) {
			return true
		}
	}
	return false
}

func placeName(a *models.Activity) string {
	if v := strings.TrimSpace(strValue(a.VenueName)); v != "" {
		return v
	}
	return strings.TrimSpace(a.Name)
}

func activityAddress(a *models.Activity) string {
	if a.Address == nil {
		return ""
	}
	if f := strings.TrimSpace(strValue(a.Address.Formatted)); f != "" {
		return f
	}
	var parts []string
	for _, s := range []*string{a.Address.Street, a.Address.Locality, a.Address.Region} {
		if v := strings.TrimSpace(strValue(s)); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ", ")
}

func strValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func renderLeg(leg travel.Leg, i int, stops []Stop, driveLabel string) Leg {
	from, to := "start", "end"
	if i > 0 && i-1 < len(stops) {
		from = stops[i-1].ID
	}
	if i < len(stops) {
		to = stops[i].ID
	}
	return Leg{
		FromStopID: from, ToStopID: to,
		Mode:       legMode(leg.Mode, driveLabel),
		Minutes:    int(math.Round(leg.Duration.Minutes())),
		DistanceKm: math.Round(leg.DistanceKm*100) / 100,
	}
}

// legMode maps the optimizer's modes onto the app's enum; transit is
// always "marta", driving is "drive" or "rideshare" from the ride answer.
func legMode(m travel.Mode, driveLabel string) string {
	switch m {
	case travel.Transit:
		return "marta"
	case travel.Drive:
		if driveLabel == "rideshare" {
			return "rideshare"
		}
		return "drive"
	}
	return "walk"
}

// legVerb labels a leg mode the way the app does (TravelMode.label).
func legVerb(mode string) string {
	switch mode {
	case "marta":
		return "MARTA"
	case "drive":
		return "Drive"
	case "rideshare":
		return "Rideshare"
	case "uber":
		return "Uber"
	}
	return "Walk"
}

// metaLegWord names the non-walking legs in an option's meta line, worded
// like the contract's example ("2 transit legs").
func metaLegWord(mode travel.Mode, driveLabel string) string {
	switch mode {
	case travel.Transit:
		return "transit"
	case travel.Drive:
		if driveLabel == "rideshare" {
			return "rideshare"
		}
		return "drive"
	}
	return "walk"
}

// --- names, tags, meta -------------------------------------------------------

// optionName is "short(s0) + short(s1)" plus "+ N more".
func optionName(stops []Stop) string {
	if len(stops) == 0 {
		return "Sidequest"
	}
	name := shortName(stops[0].Title)
	if len(stops) > 1 {
		name += " + " + shortName(stops[1].Title)
	}
	if len(stops) > 2 {
		name += fmt.Sprintf(" + %d more", len(stops)-2)
	}
	return name
}

// Words a short name should not end on ("Sunset Jazz on" → "Sunset Jazz").
var trailingConnectors = map[string]bool{
	"a": true, "an": true, "and": true, "at": true, "for": true, "in": true, "of": true,
	"on": true, "the": true, "to": true, "with": true, "&": true, "+": true, "-": true,
}

// shortName keeps the first three words, drops a leading "The", cuts at
// ":", " at " or " - ", and never ends on a connector word.
func shortName(title string) string {
	s := strings.TrimSpace(title)
	for _, sep := range []string{":", " at ", " - ", " – ", " — "} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	words := strings.Fields(s)
	if len(words) > 1 && strings.EqualFold(words[0], "the") {
		words = words[1:]
	}
	if len(words) > 3 {
		words = words[:3]
	}
	for len(words) > 1 && trailingConnectors[strings.ToLower(words[len(words)-1])] {
		words = words[:len(words)-1]
	}
	if len(words) == 0 {
		return strings.TrimSpace(title)
	}
	return strings.Join(words, " ")
}

// optionTag names an option: the first is "Best match", the others take
// the first rule that fits and is not used yet.
func optionTag(idx int, opt Option, used map[string]bool, spec PlanSpec) string {
	if idx == 0 {
		return "Best match"
	}
	facet := func(name string) bool {
		f, _ := FacetByName(name)
		n := 0
		for _, s := range opt.Stops {
			if coversFacet(s, f) {
				n++
			}
		}
		return len(opt.Stops) > 0 && n*2 >= len(opt.Stops)
	}
	rules := []struct {
		tag string
		ok  func() bool
	}{
		{"Free", func() bool { return opt.CostKnown && opt.TotalCostCents == 0 }},
		{"Outdoors", func() bool { return facet("Outdoors") }},
		{"Nightlife", func() bool { return facet("Nightlife") }},
		{"Meet people", func() bool { return facet("Meet people") }},
		{"Chill", func() bool { return len(opt.Stops) <= 2 || facet("Chill") }},
		{"Packed", func() bool { return len(opt.Stops) >= 4 }},
		{"Active", func() bool { return facet("Active") }},
		{"Culture", func() bool { return facet("Art") || facet("Nerdy") }},
	}
	for _, r := range rules {
		if !used[r.tag] && r.ok() {
			return r.tag
		}
	}
	return "Different vibe"
}

func coversFacet(s Stop, f Facet) bool {
	a := models.Activity{Category: s.Category, Tags: s.Tags}
	return f.Covers(&a)
}

// optionMeta is "{Free|~$|~$$|~$$$} · {miles} mi {walking|by transit|…} ·
// {n} {transit|drive|rideshare} legs"; the price part is left out when no
// stop's price is known, and walking-only plans count stops instead.
func optionMeta(opt Option, spec *PlanSpec, totalKm float64) string {
	var parts []string
	if price := optionPrice(opt); price != "" {
		parts = append(parts, price)
	}
	parts = append(parts, fmt.Sprintf("%.1f mi %s", totalKm/kmPerMile, travelPhrase(spec.Mode, spec.DriveLabel)))
	label := metaLegWord(spec.Mode, spec.DriveLabel)
	n := 0
	for _, l := range opt.Legs {
		if l.Mode != "walk" {
			n++
		}
	}
	switch {
	case n == 1:
		parts = append(parts, fmt.Sprintf("1 %s leg", label))
	case n > 1:
		parts = append(parts, fmt.Sprintf("%d %s legs", n, label))
	case len(opt.Stops) == 1:
		parts = append(parts, "1 stop")
	default:
		parts = append(parts, fmt.Sprintf("%d stops", len(opt.Stops)))
	}
	return strings.Join(parts, " · ")
}

// optionPrice is the price glyph of a whole option: "Free" when every stop
// is known free, else "~$".."~$$$" from the priciest known stop; empty
// when nothing is known.
func optionPrice(opt Option) string {
	if opt.CostKnown && opt.TotalCostCents == 0 && len(opt.Stops) > 0 {
		return "Free"
	}
	maxTier, known := 0, false
	for _, s := range opt.Stops {
		if s.TierKnown {
			known = true
			if s.Tier > maxTier {
				maxTier = s.Tier
			}
		}
	}
	if !known {
		return ""
	}
	if maxTier == 0 {
		return "Free"
	}
	return "~" + tierSymbol(maxTier)
}

func travelPhrase(mode travel.Mode, driveLabel string) string {
	switch mode {
	case travel.Transit:
		return "by transit"
	case travel.Drive:
		if driveLabel == "rideshare" {
			return "by rideshare"
		}
		return "by car"
	}
	return "walking"
}

func optionSummary(opt Option, spec *PlanSpec, totalKm float64) string {
	if len(opt.Stops) == 0 {
		return ""
	}
	word := "stops"
	if len(opt.Stops) == 1 {
		word = "stop"
	}
	clock := func(t time.Time) string { return t.In(spec.TZ).Format("3:04 PM") }
	return fmt.Sprintf("%d %s · %s–%s · %.1f km %s", len(opt.Stops), word,
		clock(opt.Stops[0].Arrive), clock(opt.Stops[len(opt.Stops)-1].Depart), totalKm, travelPhrase(spec.Mode, spec.DriveLabel))
}

// stopSubtitle is "{CategoryLabel} · {Free|$|$$|$$$}" (price omitted when
// unknown); events add their start time.
func stopSubtitle(s Stop, a *models.Activity, tz *time.Location) string {
	parts := []string{categoryLabel(a.Category)}
	if s.TierKnown {
		parts = append(parts, tierSymbol(s.Tier))
	}
	if a.Kind == "event" && a.Start != nil && tz != nil {
		parts = append(parts, a.Start.In(tz).Format("3:04 PM"))
	}
	return strings.Join(parts, " · ")
}

var categoryLabels = map[string]string{
	"park": "Park", "hike": "Hike", "bar": "Bar", "landmark": "Landmark", "museum": "Museum",
	"gallery": "Gallery", "garden": "Garden", "shopping": "Shopping", "rec_venue": "Games + fun",
	"zoo_aquarium": "Zoo + aquarium", "market": "Market", "viewpoint": "Views", "restaurant": "Restaurant",
	"cafe": "Café", "bakery": "Bakery", "dessert": "Dessert", "food_hall": "Food hall", "brewery": "Brewery",
	"nightclub": "Nightclub", "live_music": "Live music", "comedy": "Comedy",
	"class_workshop": "Workshop", "community_event": "Community", "festival": "Festival",
	"sports_event": "Sports", "tour": "Tour", "theater": "Theater", "cinema": "Cinema", "other": "Sidequest",
}

func categoryLabel(cat string) string {
	if l, ok := categoryLabels[cat]; ok {
		return l
	}
	if cat == "" {
		return "Sidequest"
	}
	words := strings.Fields(strings.ReplaceAll(cat, "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}
