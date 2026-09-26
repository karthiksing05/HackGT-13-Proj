//go:build e2e

package e2e

import (
	"Backend/pkg/contract"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// planRequest is the Create flow's request for a window on day.
func planRequest(start contract.Place, day time.Time, fromHour, toHour int, rng contract.TravelRange, modes contract.TravelModes, tags []string, budget int, who contract.Visibility) contract.PlanRequest {
	return contract.PlanRequest{
		Start: start, End: start, Date: contract.NewTime(at(day, fromHour, 0)),
		StartTime: contract.NewTime(at(day, fromHour, 0)), BackBy: contract.NewTime(at(day, toHour, 0)),
		Range: rng, Ride: contract.RideNone, MoodText: "chill and outside, then something to eat", Tags: tags,
		Budget: budget, Who: who, Pace: contract.PaceBalanced, Modes: modes,
	}
}

// checkBatch asserts what the planner guarantees for every option of a page.
func checkBatch(t testing.TB, req contract.PlanRequest, batch contract.PlanBatch) {
	t.Helper()
	if len(batch.Options) == 0 {
		if batch.Reason == nil || !batch.Done {
			t.Errorf("an empty batch must be done with a reason: %+v", batch)
		}
		return
	}
	if batch.Reason != nil {
		t.Errorf("reason %q with options", *batch.Reason)
	}
	if !batch.Done && batch.Cursor == nil {
		t.Error("a batch that is not done needs a cursor")
	}
	for _, opt := range batch.Options {
		if opt.ID == "" || opt.Name == "" || opt.Tag == "" || opt.Meta == "" || len(opt.Stops) == 0 {
			t.Errorf("option %+v", opt)
			continue
		}
		seen := map[string]bool{}
		var prevDepart time.Time
		for i, st := range opt.Stops {
			where := fmt.Sprintf("option %s stop %d (%s)", opt.ID, i, st.Title)
			if !strings.HasPrefix(st.ID, "stop_") || st.Title == "" || st.Subtitle == "" || st.Place.Name == "" || st.Place.Coordinate == nil ||
				st.DurationMinutes <= 0 || st.ActivityID == nil || st.ArriveTime == nil || st.DepartTime == nil || st.Kind == "" || st.Flexible == nil {
				t.Errorf("%s: incomplete %+v", where, st)
				continue
			}
			if st.ArriveTime.Before(req.StartTime.Time) || st.DepartTime.After(req.BackBy.Time) || !st.DepartTime.After(st.ArriveTime.Time) {
				t.Errorf("%s: %s–%s is outside the window %s–%s", where, st.ArriveTime, st.DepartTime, req.StartTime, req.BackBy)
			}
			if i > 0 && st.ArriveTime.Before(prevDepart) {
				t.Errorf("%s overlaps the stop before", where)
			}
			prevDepart = st.DepartTime.Time
			if seen[*st.ActivityID] {
				t.Errorf("%s repeats an activity", where)
			}
			seen[*st.ActivityID] = true
		}
	}
}

func stopIDs(stops []contract.PlanStop) []string {
	out := make([]string, len(stops))
	for i, s := range stops {
		out[i] = s.ID
	}
	return out
}

// routeFor re-times an order of an option.
func routeFor(t testing.TB, s *Session, req contract.PlanRequest, optionID string, order []string) contract.RouteResult {
	t.Helper()
	route := send[contract.RouteResult](t, s, "POST", "/plans/route", contract.RouteRequest{
		OptionID: optionID, StopOrder: order, Start: req.Start, End: req.End, StartTime: req.StartTime, BackBy: req.BackBy,
		Ride: req.Ride, Modes: req.Modes,
	}, http.StatusOK)
	if len(route.Legs) != len(order)+1 || len(route.StopTimes) != len(order) || route.MinutesLate < 0 || route.BrokenAt < -1 || route.BrokenAt >= len(order) {
		t.Errorf("route for %d stops: %d legs, %d stop times, late %d, broken at %d", len(order), len(route.Legs), len(route.StopTimes), route.MinutesLate, route.BrokenAt)
	}
	prev := req.StartTime.Time
	for i, w := range route.StopTimes {
		if w.Start.Before(prev) || !w.End.After(w.Start.Time) {
			t.Errorf("stop time %d %s–%s is out of order", i, w.Start, w.End)
		}
		prev = w.End.Time
	}
	for i, leg := range route.Legs {
		if leg.Minutes < 0 {
			t.Errorf("leg %d: %+v", i, leg)
		}
	}
	if route.Arrival.Before(prev) {
		t.Errorf("arrival %s before the last stop ends %s", route.Arrival, prev)
	}
	return route
}

// generate asks for plans and checks the page.
func generate(t testing.TB, s *Session, req contract.PlanRequest) contract.PlanBatch {
	t.Helper()
	batch := send[contract.PlanBatch](t, s, "POST", "/plans/generate", req, http.StatusOK)
	checkBatch(t, req, batch)
	return batch
}

func stopItems(it contract.Itinerary) []contract.ItineraryItem {
	var out []contract.ItineraryItem
	for _, item := range it.Items {
		if item.Kind == contract.KindSidequest || item.Kind == contract.KindGroup {
			out = append(out, item)
		}
	}
	return out
}

// TestDemoPlanAndSidequest plans tomorrow afternoon in Saltlight Harbor,
// swaps a stop, saves the sidequest, edits it every way the app can, gets
// tickets through the checkout agent, and deletes it.
func TestDemoPlanAndSidequest(t *testing.T) {
	s := sandy(t)
	c := config(t)
	me := get[contract.User](t, s, "/me")
	home := *me.HomeBase
	tomorrow := demoToday(t, s).AddDate(0, 0, 1)
	req := planRequest(home, tomorrow, 12, 16, contract.RangeWalkable, contract.TravelModes{contract.ModeWalk},
		[]string{"Outdoors", "Music"}, 1, contract.VisibilityFriends)

	started := time.Now()
	batch := generate(t, s, req)
	t.Logf("generate took %s: %d options", time.Since(started).Round(time.Millisecond), len(batch.Options))
	if len(batch.Options) == 0 {
		t.Fatalf("no plans for tomorrow 12–4 PM in Saltlight: %v", batch.Reason)
	}
	if batch.Options[0].Tag != "Best match" {
		t.Errorf("first option's tag %q", batch.Options[0].Tag)
	}
	options := slices.Clone(batch.Options)
	if batch.Cursor != nil {
		more := send[contract.PlanBatch](t, s, "POST", "/plans/generate/more", contract.MoreRequest{Cursor: *batch.Cursor}, http.StatusOK)
		checkBatch(t, req, more)
		for _, o := range more.Options {
			if slices.ContainsFunc(options, func(p contract.PlanOption) bool { return p.ID == o.ID }) {
				t.Errorf("more repeated option %s", o.ID)
			}
		}
		options = append(options, more.Options...)
	}
	junk := send[contract.PlanBatch](t, s, "POST", "/plans/generate/more", contract.MoreRequest{Cursor: "dag_nope_3"}, http.StatusOK)
	if len(junk.Options) != 0 || !junk.Done {
		t.Errorf("an unknown cursor: %+v", junk)
	}

	// The window already ended, or an enum the app never sends.
	ended := req
	ended.StartTime = contract.NewTime(time.Now().Add(-5 * time.Hour))
	ended.BackBy = contract.NewTime(time.Now().Add(-3 * time.Hour))
	ended.Date = ended.StartTime
	gone := send[contract.PlanBatch](t, s, "POST", "/plans/generate", ended, http.StatusOK)
	if len(gone.Options) != 0 || !gone.Done || gone.Reason == nil || !strings.HasPrefix(*gone.Reason, "invalid_request") {
		t.Errorf("a window in the past: %+v", gone)
	}
	bad := req
	bad.Range = "teleport"
	fails(t, s, "POST", "/plans/generate", bad, http.StatusBadRequest)

	// Prefer an option with a priced stop (the checkout below needs one).
	pick := options[0]
	for _, o := range options {
		if o.TotalCostCents != nil && *o.TotalCostCents > 0 && len(o.Stops) >= 2 {
			pick = o
			break
		}
	}
	order := stopIDs(pick.Stops)
	routeFor(t, s, req, pick.ID, order)

	// Swap the first stop for an alternative.
	alts := send[[]contract.PlanAlternative](t, s, "POST", "/plans/alternatives",
		contract.AlternativesRequest{OptionID: pick.ID, StopID: order[0], StopOrder: order}, http.StatusOK)
	if len(alts) > 5 {
		t.Errorf("%d alternatives, at most 5", len(alts))
	}
	for _, a := range alts {
		if !strings.HasPrefix(a.Stop.ID, "alt_") || a.Reason == "" || slices.Contains(order, a.Stop.ID) || a.Stop.Place.Coordinate == nil {
			t.Errorf("alternative %+v", a)
		}
		for _, st := range pick.Stops {
			if a.Stop.ActivityID != nil && st.ActivityID != nil && *a.Stop.ActivityID == *st.ActivityID {
				t.Errorf("alternative %s is already in the plan", a.Stop.Title)
			}
		}
	}
	edited := pick
	edited.Stops = slices.Clone(pick.Stops)
	if len(alts) > 0 {
		edited.Stops[0] = alts[0].Stop
	} else {
		t.Log("no alternatives for the first stop; saving the option as generated")
	}
	finalOrder := stopIDs(edited.Stops)
	route := routeFor(t, s, req, pick.ID, finalOrder)
	if len(finalOrder) > 1 {
		reversed := slices.Clone(finalOrder)
		slices.Reverse(reversed)
		routeFor(t, s, req, pick.ID, reversed)
	}
	if msg := fails(t, s, "POST", "/plans/route", contract.RouteRequest{OptionID: pick.ID, StopOrder: []string{"stop_nope_0"}, Start: req.Start,
		End: req.End, StartTime: req.StartTime, BackBy: req.BackBy, Ride: req.Ride, Modes: req.Modes}, http.StatusBadRequest); msg != "That plan changed. Go back and try again." {
		t.Errorf("unknown stop: %q", msg)
	}
	if msg := fails(t, s, "POST", "/plans/route", contract.RouteRequest{OptionID: "nope-0", StopOrder: finalOrder, Start: req.Start,
		End: req.End, StartTime: req.StartTime, BackBy: req.BackBy, Ride: req.Ride, Modes: req.Modes}, http.StatusNotFound); msg != "This plan expired. Generate again." {
		t.Errorf("unknown option: %q", msg)
	}
	alice, _ := people(t)
	fails(t, alice, "POST", "/plans/alternatives", contract.AlternativesRequest{OptionID: pick.ID, StopID: order[0], StopOrder: order}, http.StatusNotFound)

	// Save it.
	ws := dial(t, s)
	created := send[contract.Itinerary](t, s, "POST", "/itineraries", contract.CreateItineraryRequest{
		Plan: req, Option: edited, StopOrder: finalOrder, Route: route, Visibility: contract.VisibilityJustMe,
	}, http.StatusCreated)
	t.Cleanup(func() { do(t, s, "DELETE", "/itineraries/"+created.ID, nil) })
	stops := stopItems(created)
	if !created.IsHost || created.GoingCount != 1 || created.Visibility != contract.VisibilityJustMe || len(stops) != len(finalOrder) ||
		len(created.Items) != 2*len(finalOrder)+1 || created.Title == "" {
		t.Fatalf("saved sidequest: host %v, going %d, %s, %d stops of %d items", created.IsHost, created.GoingCount, created.Visibility, len(stops), len(created.Items))
	}
	for i, item := range created.Items {
		wantKind := contract.KindTransit
		if i%2 == 1 { // a solo plan's stops are sidequest blocks
			wantKind = contract.KindSidequest
		}
		if item.Kind != wantKind {
			t.Errorf("item %d is %s, want %s", i, item.Kind, wantKind)
		}
		if item.Kind == contract.KindTransit && !strings.Contains(item.Title, " to ") {
			t.Errorf("transit item title %q", item.Title)
		}
	}
	for i, st := range stops {
		if st.Title != edited.Stops[i].Title || st.ActivityID == nil || *st.ActivityID != *edited.Stops[i].ActivityID {
			t.Errorf("stop %d is %q, want %q", i, st.Title, edited.Stops[i].Title)
		}
		if !st.Start.Equal(route.StopTimes[i].Start.Time) || !st.End.Equal(route.StopTimes[i].End.Time) {
			t.Errorf("stop %d at %s–%s, the route said %s–%s", i, st.Start, st.End, route.StopTimes[i].Start, route.StopTimes[i].End)
		}
		if len(st.People) != 0 || st.Kind != contract.KindSidequest {
			t.Errorf("a solo stop %d is a sidequest without people: %s %+v", i, st.Kind, st.People)
		}
	}
	if y, m, d := created.Date.In(c.Loc).Date(); time.Date(y, m, d, 0, 0, 0, 0, c.Loc) != tomorrow {
		t.Errorf("date %s, want %s", created.Date, tomorrow.Format("2006-01-02"))
	}
	if created.StartPlace.Name != home.Name || created.EndPlace.Name != home.Name {
		t.Errorf("start/end places: %q → %q", created.StartPlace.Name, created.EndPlace.Name)
	}
	listed := get[[]contract.Itinerary](t, s, "/itineraries?status=active")
	if !slices.ContainsFunc(listed, func(it contract.Itinerary) bool { return it.ID == created.ID }) {
		t.Error("the new sidequest is not on Home")
	}
	fails(t, s, "GET", "/itineraries?status=someday", nil, http.StatusBadRequest)
	fails(t, alice, "GET", "/itineraries/"+created.ID, nil, http.StatusNotFound)
	days := get[[]contract.CalendarDay](t, s, "/calendar/days?from="+tomorrow.Format("2006-01-02")+"&to="+tomorrow.Format("2006-01-02"))
	onDay := 0
	for _, item := range days[0].Items {
		if item.ItineraryID != nil && *item.ItineraryID == created.ID {
			onDay++
		}
	}
	if onDay != len(stops) {
		t.Errorf("%d of %d stops on the calendar", onDay, len(stops))
	}
	word := strings.Fields(stops[0].Title)[0]
	if found := get[contract.SearchResults](t, s, "/search?q="+word); !slices.ContainsFunc(found.Sidequests, func(it contract.Itinerary) bool { return it.ID == created.ID }) {
		t.Errorf("search %q does not find the sidequest", word)
	}

	// Rename, reorder and move a day; every edit re-renders for the host.
	path := "/itineraries/" + created.ID
	title := "E2E golden afternoon"
	renamed := send[contract.Itinerary](t, s, "PATCH", path, contract.ItineraryUpdate{Title: &title}, http.StatusOK)
	if renamed.Title != title || len(renamed.Items) != len(created.Items) {
		t.Errorf("rename: %q with %d items", renamed.Title, len(renamed.Items))
	}
	var upd contract.Itinerary
	expectEvent(t, ws, "itinerary.updated", 10*time.Second, &upd, func(v contract.Itinerary) bool { return v.ID == created.ID && v.Title == title })
	if len(stops) > 1 {
		ids := make([]string, len(stops))
		for i, st := range stops {
			ids[len(stops)-1-i] = st.ID
		}
		reordered := send[contract.Itinerary](t, s, "PATCH", path, contract.ItineraryUpdate{StopOrder: ids}, http.StatusOK)
		got := stopItems(reordered)
		if len(got) != len(ids) || got[0].ID != ids[0] || got[len(got)-1].ID != ids[len(ids)-1] {
			t.Errorf("reorder: %v", got)
		}
		if !got[0].Start.After(reordered.Start.Time) && !got[0].Start.Equal(reordered.Start.Time) {
			t.Errorf("reordered first stop starts before the plan")
		}
		// Leaving a stop out removes it.
		trimmed := send[contract.Itinerary](t, s, "PATCH", path, contract.ItineraryUpdate{StopOrder: ids[:len(ids)-1]}, http.StatusOK)
		if n := len(stopItems(trimmed)); n != len(ids)-1 {
			t.Errorf("after removing a stop: %d stops", n)
		}
	}
	current := get[contract.Itinerary](t, s, path)
	next := tomorrow.AddDate(0, 0, 1)
	day := contract.NewTime(next)
	moved := send[contract.Itinerary](t, s, "PATCH", path, contract.ItineraryUpdate{Date: &day}, http.StatusOK)
	if moved.Start.Sub(current.Start.Time) != 24*time.Hour || moved.BackBy.Sub(current.BackBy.Time) != 24*time.Hour {
		t.Errorf("moving a day: start %s → %s", current.Start, moved.Start)
	}
	for i := range moved.Items {
		if i < len(current.Items) && moved.Items[i].Start.Sub(current.Items[i].Start.Time) != 24*time.Hour {
			t.Errorf("item %d moved by %s", i, moved.Items[i].Start.Sub(current.Items[i].Start.Time))
		}
	}
	if y, m, d := moved.Date.In(c.Loc).Date(); time.Date(y, m, d, 0, 0, 0, 0, c.Loc) != next {
		t.Errorf("date after the move %s", moved.Date)
	}
	empty := ""
	if msg := fails(t, s, "PATCH", path, contract.ItineraryUpdate{Title: &empty}, http.StatusBadRequest); msg != "Give your sidequest a name." {
		t.Errorf("empty title: %q", msg)
	}
	fails(t, s, "PATCH", path, rawJSON(`{"stop_order": []}`), http.StatusBadRequest)
	early := contract.NewTime(moved.Start.Add(-time.Hour))
	fails(t, s, "PATCH", path, contract.ItineraryUpdate{BackBy: &early}, http.StatusBadRequest)
	fails(t, s, "PATCH", path, rawJSON(`{"visibility": "everyone"}`), http.StatusBadRequest)

	// Notes, transit and the Event sheet on a stop.
	stop := stopItems(moved)[0]
	itemPath := path + "/items/" + stop.ID
	private := contract.NotesPrivate
	do(t, s, "PATCH", itemPath, contract.ItemNotesPatch{Notes: "Sunscreen.", NotesScope: &private}).NoContent(t)
	ev := get[contract.ItineraryItem](t, s, "/events/"+stop.ID)
	if ev.Notes == nil || *ev.Notes != "Sunscreen." || ev.NotesScope == nil || *ev.NotesScope != contract.NotesPrivate || ev.Title != stop.Title ||
		ev.Kind != contract.KindSidequest || ev.Place == nil || ev.ActivityID == nil {
		t.Errorf("event sheet: %+v", ev)
	}
	do(t, s, "PATCH", "/events/"+stop.ID, contract.ItemNotesPatch{Notes: "Sunscreen and water."}).NoContent(t)
	if ev = get[contract.ItineraryItem](t, s, "/events/"+stop.ID); ev.Notes == nil || *ev.Notes != "Sunscreen and water." || *ev.NotesScope != contract.NotesPrivate {
		t.Errorf("notes without a scope keep the saved one: %v %v", ev.Notes, ev.NotesScope)
	}
	fails(t, s, "PATCH", itemPath, contract.ItemNotesPatch{Notes: strings.Repeat("x", 2001)}, http.StatusBadRequest)
	fails(t, s, "PATCH", itemPath, rawJSON(`{"notes": "x", "notes_scope": "public"}`), http.StatusBadRequest)
	opts := get[[]contract.TransitOption](t, s, itemPath+"/transit")
	if len(opts) != 3 || opts[0].Mode != contract.ModeWalk || opts[1].Mode != contract.ModeMarta || opts[2].Mode != contract.ModeRideshare ||
		opts[0].CostCents == nil || *opts[0].CostCents != 0 || opts[1].CostCents == nil || *opts[1].CostCents != 250 || opts[2].CostCents != nil {
		t.Errorf("transit options: %+v", opts)
	}
	if via := get[[]contract.TransitOption](t, s, "/events/"+stop.ID+"/transit"); len(via) != 3 || via[0].Minutes != opts[0].Minutes {
		t.Errorf("/events transit differs: %+v", via)
	}
	do(t, s, "PUT", itemPath+"/transit", contract.TransitSelection{Mode: contract.ModeMarta}).NoContent(t)
	if ev = get[contract.ItineraryItem](t, s, "/events/"+stop.ID); ev.TransitMode == nil || *ev.TransitMode != contract.ModeMarta {
		t.Errorf("transit_mode after selecting: %v", ev.TransitMode)
	}
	fails(t, s, "PUT", itemPath+"/transit", contract.TransitSelection{Mode: "teleport"}, http.StatusBadRequest)
	fails(t, s, "GET", path+"/items/no-such-item/transit", nil, http.StatusNotFound)

	// Tickets through the checkout agent (with approval).
	checkoutFlow(t, s, moved)

	// Sharing it puts it on the Forum for people nearby; deleting removes it.
	open := contract.VisibilityOpen
	shared := send[contract.Itinerary](t, s, "PATCH", path, contract.ItineraryUpdate{Visibility: &open}, http.StatusOK)
	if shared.Visibility != contract.VisibilityOpen {
		t.Errorf("visibility %s", shared.Visibility)
	}
	seenBy := forum(t, alice, fmt.Sprintf("?lat=%f&lng=%f&radius=5", home.Coordinate.Lat, home.Coordinate.Lng))
	if !slices.ContainsFunc(seenBy, func(p contract.ForumPost) bool { return p.ID == created.ID && p.Author.ID == s.UserID }) {
		t.Errorf("an open plan is not on the Forum for someone nearby: %v", postIDs(seenBy))
	}
	do(t, s, "DELETE", path, nil).NoContent(t)
	var removed itineraryRemoved
	expectEvent(t, ws, "itinerary.removed", 10*time.Second, &removed, func(v itineraryRemoved) bool { return v.ItineraryID == created.ID })
	fails(t, s, "GET", path, nil, http.StatusNotFound)
	if slices.ContainsFunc(forum(t, alice, fmt.Sprintf("?lat=%f&lng=%f&radius=5", home.Coordinate.Lat, home.Coordinate.Lng)),
		func(p contract.ForumPost) bool { return p.ID == created.ID }) {
		t.Error("a deleted plan stays on the Forum")
	}
	fails(t, s, "DELETE", path, nil, http.StatusNotFound)
}

type checkoutStatus struct {
	IntentID string                 `json:"intent_id"`
	State    contract.CheckoutState `json:"state"`
}

// pollIntent waits until the intent reaches state.
func pollIntent(t testing.TB, s *Session, id string, state contract.CheckoutState) contract.CheckoutIntent {
	t.Helper()
	var intent contract.CheckoutIntent
	waitFor(t, 20*time.Second, "checkout "+id+" to be "+string(state), func() bool {
		intent = get[contract.CheckoutIntent](t, s, "/checkout/intents/"+id)
		return intent.State == state
	})
	return intent
}

// checkoutFlow buys tickets for a stop of it with approval, and checks the
// guards around it.
func checkoutFlow(t *testing.T, s *Session, it contract.Itinerary) {
	t.Helper()
	stops := stopItems(it)
	item := stops[0]
	for _, st := range stops {
		if st.PriceCents != nil && *st.PriceCents > 0 {
			item = st
			break
		}
	}
	ws := dial(t, s)
	fails(t, s, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: "", Quantity: 1}, http.StatusBadRequest)
	fails(t, s, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: item.ID, Quantity: 11}, http.StatusBadRequest)
	fails(t, s, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: "no-such-item", Quantity: 1}, http.StatusNotFound)

	intent := send[contract.CheckoutIntent](t, s, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: item.ID, Quantity: 2}, http.StatusCreated)
	if intent.State != contract.CheckoutPreparing || intent.ItemID != item.ID || intent.ItemTitle != item.Title || intent.Quantity != 2 ||
		intent.CardBrand == "" || intent.CardLast4 == "" || intent.PaymentMethodID == nil || len(intent.Steps) != 3 || intent.Instant {
		t.Fatalf("new intent: %+v", intent)
	}
	if item.PriceCents != nil && (intent.SubtotalCents == nil || *intent.SubtotalCents != 2**item.PriceCents) {
		t.Errorf("subtotal %v for 2 × %d", intent.SubtotalCents, *item.PriceCents)
	}
	var st checkoutStatus
	expectEvent(t, ws, "checkout.status", 10*time.Second, &st, func(v checkoutStatus) bool { return v.IntentID == intent.ID && v.State == contract.CheckoutPreparing })
	if msg := fails(t, s, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, http.StatusBadRequest); !strings.Contains(msg, "still getting the quote") {
		t.Errorf("approve while preparing: %q", msg)
	}
	quoted := pollIntent(t, s, intent.ID, contract.CheckoutAwaitingApproval)
	expectEvent(t, ws, "checkout.status", 10*time.Second, &st, func(v checkoutStatus) bool {
		return v.IntentID == intent.ID && v.State == contract.CheckoutAwaitingApproval
	})
	if !quoted.Steps[0].Done || !quoted.Steps[1].Done || quoted.Steps[2].Done {
		t.Errorf("quoted steps: %+v", quoted.Steps)
	}
	if quoted.SubtotalCents != nil && (quoted.TotalCents == nil || quoted.FeesCents == nil || *quoted.TotalCents != *quoted.SubtotalCents+*quoted.FeesCents) {
		t.Errorf("quote: subtotal %v fees %v total %v", quoted.SubtotalCents, quoted.FeesCents, quoted.TotalCents)
	}
	nope := "no-such-card"
	fails(t, s, "PATCH", "/checkout/intents/"+intent.ID, contract.CheckoutPatch{PaymentMethodID: &nope}, http.StatusNotFound)
	fails(t, s, "PATCH", "/checkout/intents/"+intent.ID, contract.CheckoutPatch{}, http.StatusBadRequest)
	card := *quoted.PaymentMethodID
	same := send[contract.CheckoutIntent](t, s, "PATCH", "/checkout/intents/"+intent.ID, contract.CheckoutPatch{PaymentMethodID: &card}, http.StatusOK)
	if same.State != contract.CheckoutAwaitingApproval {
		t.Errorf("changing the card moved the intent to %s", same.State)
	}
	alice, _ := people(t)
	fails(t, alice, "GET", "/checkout/intents/"+intent.ID, nil, http.StatusNotFound)
	fails(t, alice, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, http.StatusNotFound)

	approved := send[contract.CheckoutIntent](t, s, "POST", "/checkout/intents/"+intent.ID+"/approve", nil, http.StatusOK)
	if approved.State != contract.CheckoutProcessing && approved.State != contract.CheckoutBooked {
		t.Errorf("after approving: %s", approved.State)
	}
	booked := pollIntent(t, s, intent.ID, contract.CheckoutBooked)
	expectEvent(t, ws, "checkout.status", 10*time.Second, &st, func(v checkoutStatus) bool { return v.IntentID == intent.ID && v.State == contract.CheckoutBooked })
	for _, step := range booked.Steps {
		if !step.Done {
			t.Errorf("booked with an open step: %+v", booked.Steps)
		}
	}
	if msg := fails(t, s, "POST", "/checkout/intents/"+intent.ID+"/cancel", nil, http.StatusConflict); msg != "This checkout already went through." {
		t.Errorf("cancel after booking: %q", msg)
	}
	fails(t, s, "PATCH", "/checkout/intents/"+intent.ID, contract.CheckoutPatch{PaymentMethodID: &card}, http.StatusConflict)
	ticketed := get[contract.ItineraryItem](t, s, "/events/"+item.ID)
	if ticketed.Ticket == nil || ticketed.Ticket.Quantity != 2 || ticketed.Ticket.Confirmation == nil || !strings.HasPrefix(*ticketed.Ticket.Confirmation, "SQ-") ||
		ticketed.Ticket.URL == nil {
		t.Fatalf("ticket on the item: %+v", ticketed.Ticket)
	}
	page := do(t, nil, "GET", *ticketed.Ticket.URL, nil).Expect(t, http.StatusOK)
	if !strings.Contains(string(page.Body), *ticketed.Ticket.Confirmation) || !strings.Contains(page.Header.Get("Content-Type"), "text/html") {
		t.Errorf("ticket page lacks the confirmation %s", *ticketed.Ticket.Confirmation)
	}
	do(t, nil, "GET", "/tickets/not-a-ticket", nil).Expect(t, http.StatusNotFound)

	// A second checkout, cancelled before approval.
	other := send[contract.CheckoutIntent](t, s, "POST", "/checkout/intents", contract.CreateCheckoutIntent{ItemID: item.ID, Quantity: 1}, http.StatusCreated)
	do(t, s, "POST", "/checkout/intents/"+other.ID+"/cancel", nil).NoContent(t)
	if got := get[contract.CheckoutIntent](t, s, "/checkout/intents/"+other.ID); got.State != contract.CheckoutCancelled {
		t.Errorf("cancelled intent is %s", got.State)
	}
	do(t, s, "POST", "/checkout/intents/"+other.ID+"/cancel", nil).NoContent(t)
	if msg := fails(t, s, "POST", "/checkout/intents/"+other.ID+"/approve", nil, http.StatusBadRequest); msg != "This checkout already finished." {
		t.Errorf("approve a cancelled intent: %q", msg)
	}
	time.Sleep(2 * time.Second) // the agent must not revive a cancelled intent
	if got := get[contract.CheckoutIntent](t, s, "/checkout/intents/"+other.ID); got.State != contract.CheckoutCancelled {
		t.Errorf("the agent moved a cancelled intent to %s", got.State)
	}
}
