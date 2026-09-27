package main

import (
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/travel"
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// The default upcoming sidequests of a person's baseline, so Home is never
// empty after a reset: one they host with a showcase friend or two and a
// short group chat, and one a showcase person hosts that they are in, both
// in the next few days, fitting their story and clear of their class
// schedule. They carry seed: "baseline-v1" with the person's id, and every
// --history --apply and --reset makes them again with fresh dates (and
// takes away whatever had gathered on the old ones).
const baselineTag = "baseline-v1"

// upcomingPlan is one default upcoming sidequest. host "" is the person.
type upcomingPlan struct {
	key, title, mood string
	host             string   // a showcase person (atlanta cast key), or "" for the person
	members          []string // showcase people who are in it besides the host
	visibility       string
	maxSize          int
	anchor           travel.Point
	stops            []stopSpec
	names            []int    // the stops its title names
	slots            [][3]int // days from today, hour, minute: the first free and open one is used
	chat             []chatLine
}

func slotsAt(hour, minute int) [][3]int {
	return [][3]int{{1, hour, minute}, {2, hour, minute}, {3, hour, minute}, {4, hour, minute}}
}

var upcomingByFlavor = map[string][]upcomingPlan{
	"outdoors": {
		{key: "host", title: "Sunset picnic in Piedmont Park", mood: "blankets, snacks, then dinner", members: []string{"diego", "sofia"},
			visibility: models.VisibilityOpen, maxSize: 6, anchor: piedmontPark, names: []int{0}, slots: slotsAt(17, 30),
			stops: []stopSpec{stop(75, parks, "Piedmont Park"), stop(60, eats, "Sugar Factory American Brasserie")},
			chat: []chatLine{
				{"diego", 3 * time.Hour, "I'll bring the frisbee. {s1} at {start} {when}?"},
				{"sofia", 2*time.Hour + 20*time.Minute, "In! Dinner at {s2} after"},
			}},
		{key: "join", title: "Eastside Trail to Ponce City Market", mood: "a walk and the food hall", host: "sofia", members: []string{"diego"},
			visibility: models.VisibilityOpen, maxSize: 8, anchor: ponceCityMarket, names: []int{0, 1}, slots: slotsAt(16, 0),
			stops: []stopSpec{stop(75, []string{"hike", "park"}, "Atlanta Beltline Eastside Trail"), stop(60, []string{"market", "restaurant"}, "Ponce City Market")},
			chat: []chatLine{
				{"sofia", 5 * time.Hour, "Walking the Eastside Trail {when} at {start}, then {s2}. Who's in?"},
				{"diego", 4 * time.Hour, "Me. Saving room for tacos"},
			}},
	},
	"nightlife": {
		{key: "host", title: "Blues night in Virginia-Highland", mood: "wings, then live blues", members: []string{"jalen", "marcus"},
			visibility: models.VisibilityOpen, maxSize: 6, anchor: virginiaHighland, slots: slotsAt(19, 30),
			stops: []stopSpec{stop(60, eats, "Moe's and Joe's"), stop(120, night, "Blind Willie's")},
			chat: []chatLine{
				{"jalen", 3 * time.Hour, "Wings at {s1} at {start} {when}, then the band at {s2}"},
				{"marcus", 90 * time.Minute, "I'm there. First round's on me"},
			}},
		{key: "join", title: "Duckpin bowling on the Westside", mood: "bowling, then blues", host: "marcus", members: []string{"amara"},
			visibility: models.VisibilityOpen, maxSize: 8, anchor: westsideAnchor, names: []int{0}, slots: slotsAt(19, 0),
			stops: []stopSpec{stop(90, games, "The Painted Duck"), stop(90, night, "Northside Tavern")},
			chat: []chatLine{
				{"marcus", 6 * time.Hour, "Lanes at {s1} {when} at {start}. Grabbing two lanes"},
				{"amara", 5 * time.Hour, "Blues at {s2} after, obviously"},
			}},
	},
	"arts": {
		{key: "host", title: "Design museum, then dinner", mood: "an hour of design, then food", members: []string{"hana", "lily"},
			visibility: models.VisibilityFriends, anchor: piedmontPark, slots: slotsAt(17, 0),
			stops: []stopSpec{stop(60, arts, "MODA (Museum of Design Atlanta)"), stop(75, eats, "Sugar Factory American Brasserie")},
			chat: []chatLine{
				{"hana", 3 * time.Hour, "{s1} closes at seven, so {start} {when}?"},
				{"lily", 2 * time.Hour, "Perfect. Dinner at {s2} after"},
			}},
		{key: "join", title: "Galleries and the Krog Street Tunnel", mood: "street art and a late lunch", host: "hana", members: []string{"omar"},
			visibility: models.VisibilityOpen, maxSize: 6, anchor: cabbagetown, names: []int{1}, slots: slotsAt(15, 30),
			stops: []stopSpec{stop(60, arts, "ABV Gallery"), stop(30, []string{"landmark", "gallery"}, "Krog Street Tunnel"), stop(60, eats, "Milltown Arms Tavern")},
			chat: []chatLine{
				{"hana", 5 * time.Hour, "Gallery hop {when} from {start}: {s1}, then the tunnel"},
				{"omar", 4 * time.Hour, "Bringing my camera"},
			}},
	},
}

// upcomingDoc is one default upcoming sidequest with its chat.
type upcomingDoc struct {
	plan     *models.Itinerary
	thread   *models.Thread
	messages []*models.Message
	who      []string // names of the others in it
	host     string   // the host's name
}

func upcomingID(uid, key string) string { return fmt.Sprintf("%s-%s-%s", baselineTag, uid, key) }

// planUpcoming lays out a person's default upcoming sidequests as of now:
// each on the first of its slots that is open, has ended before nothing it
// should not (the person's classes and blocks, the other default plan) and
// starts at least leadTime from now. Showcase people who are not there are
// left out, and so is a plan whose host is not.
func planUpcoming(hp *histPerson, cat *catalog, crew map[string]*models.User, now time.Time, loc *time.Location) ([]upcomingDoc, []string) {
	uid := hp.uid()
	busy := calendarEvents(uid, schedules[hp.flavor.key])
	var docs []upcomingDoc
	var notes []string
	used := map[string]bool{}
	for i := range upcomingByFlavor[hp.flavor.key] {
		up := &upcomingByFlavor[hp.flavor.key][i]
		host, hostName := uid, hp.user.Name
		if up.host != "" {
			u := crew[up.host]
			if u == nil {
				notes = append(notes, fmt.Sprintf("“%s” left out: its host (a showcase person) is not in the database", up.title))
				continue
			}
			host, hostName = u.ID.Hex(), u.Name
		}
		members := []string{host}
		var who []string
		if host != uid {
			members = append(members, uid)
		}
		for _, key := range up.members {
			if u := crew[key]; u != nil {
				members = append(members, u.ID.Hex())
				who = append(who, u.Name)
			}
		}
		lay, ok := laySlot(cat, up, now, loc, used, busy, docs)
		if !ok {
			notes = append(notes, fmt.Sprintf("“%s” left out: no free, open time in the next four days", up.title))
			continue
		}
		docs = append(docs, upcomingDocs(uid, up, lay, host, members, now, loc, crew))
		docs[len(docs)-1].who, docs[len(docs)-1].host = who, hostName
	}
	return docs, notes
}

// laySlot is the first slot of up that is at least leadTime away, open for
// every stop, and clear of busy blocks and the plans already laid out.
func laySlot(cat *catalog, up *upcomingPlan, now time.Time, loc *time.Location, used map[string]bool,
	busy []models.CalendarEvent, taken []upcomingDoc) (laidOut, bool) {
	local := now.In(loc)
	reach := &worldSpec{center: up.anchor, maxFromCenter: historyReach}
	for _, s := range up.slots {
		start := time.Date(local.Year(), local.Month(), local.Day()+s[0], s[1], s[2], 0, 0, loc)
		if start.Before(now.Add(leadTime)) {
			continue
		}
		lay, ok := cat.layOutAt(reach, up.anchor, up.stops, start, loc, map[string]bool{})
		if !ok || overlaps(lay.start, lay.backBy, busy, taken) {
			continue
		}
		for _, st := range lay.stops {
			used[st.place.ID.Hex()] = true
		}
		return lay, true
	}
	return laidOut{}, false
}

func overlaps(start, end time.Time, busy []models.CalendarEvent, taken []upcomingDoc) bool {
	for _, e := range busy {
		if e.Start.Before(end) && start.Before(e.End) {
			return true
		}
	}
	for _, d := range taken {
		if d.plan.Start.Before(end) && start.Before(d.plan.BackBy) {
			return true
		}
	}
	return false
}

// upcomingDocs are the plan, its group chat and messages. The person has
// not read the chat yet.
func upcomingDocs(uid string, up *upcomingPlan, lay laidOut, host string, members []string, now time.Time, loc *time.Location,
	crew map[string]*models.User) upcomingDoc {
	planID := upcomingID(uid, up.key)
	threadID := planID + "-chat"
	title := up.title
	if !lay.exact || !titleNamesHold(up.names, lay) {
		title = "Evening at " + lay.stops[0].place.Name
		if lay.start.In(loc).Hour() < 17 {
			title = "Afternoon at " + lay.stops[0].place.Name
		}
	}
	startLocal := lay.start.In(loc)
	date := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	created := now.Add(-8 * time.Hour).UTC()
	plan := &models.Itinerary{
		ID: planID, HostID: host, MemberIDs: members, Title: title,
		DateKey: date.Format("2006-01-02"), TZ: loc.String(), Date: date.UTC(), Start: lay.start.UTC(), BackBy: lay.backBy.UTC(),
		StartPlace: placeDoc(lay.stops[0].place), EndPlace: placeDoc(lay.stops[len(lay.stops)-1].place),
		Visibility: up.visibility, Items: items(planID, lay.stops),
		Plan: &models.PlanSnapshot{Range: "walkable", Ride: "none", MoodText: up.mood, Tags: planTags(lay.stops), Budget: planBudget(lay.stops),
			Who: up.visibility, Pace: "balanced", Modes: []string{"walk"}},
		RouteMode: "walk", Status: models.ItineraryActive, ThreadID: threadID, CreatedAt: created, UpdatedAt: created,
	}
	if up.maxSize > 0 {
		size := up.maxSize
		plan.MaxGroupSize = &size
	}
	if lock := lay.start.Add(-30 * time.Minute); up.visibility == models.VisibilityOpen && lock.After(now.Add(15*time.Minute)) {
		lockAt := lock.UTC()
		plan.LockAt = &lockAt
	}
	var msgs []*models.Message
	for i, line := range up.chat {
		u := crew[line.from]
		if u == nil {
			continue
		}
		sent := now.Add(-line.ago).UTC()
		msgs = append(msgs, &models.Message{ID: fmt.Sprintf("%s-msg-%d", planID, i+1), ThreadID: threadID, SenderID: u.ID.Hex(),
			Text: chatText(line.text, lay, sent, loc), SentAt: sent})
	}
	thread := &models.Thread{ID: threadID, IsGroup: true, Title: title, MemberIDs: members, ItineraryID: planID, CreatedBy: host,
		LastMessageAt: created, Unread: map[string]int{}, ReadAt: map[string]time.Time{}, CreatedAt: created, UpdatedAt: created}
	if n := len(msgs); n > 0 {
		last := msgs[n-1]
		thread.LastMessageText, thread.LastSenderID, thread.LastMessageAt, thread.UpdatedAt = last.Text, last.SenderID, last.SentAt, last.SentAt
	}
	for _, m := range members {
		thread.Unread[m], thread.ReadAt[m] = 0, thread.LastMessageAt
		if m == uid {
			thread.Unread[m], thread.ReadAt[m] = len(msgs), created
		}
	}
	return upcomingDoc{plan: plan, thread: thread, messages: msgs}
}

// titleNamesHold reports whether the stops a title names are those very places.
func titleNamesHold(names []int, lay laidOut) bool {
	for _, want := range names {
		ok := false
		for k, slot := range lay.slots {
			if slot == want && lay.stops[k].tier == 0 {
				ok = true
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// clearUpcoming deletes a person's default upcoming sidequests and all that
// gathered on them: chats and their messages, expenses and photos, notes and
// tickets, ratings, join records, "Plan together" marks, checkouts.
func clearUpcoming(ctx context.Context, st *store.Store, uid string) error {
	var plans []string
	if err := st.Collection(store.CollItineraries).Distinct(ctx, "_id", bson.M{fieldSeed: baselineTag, fieldFor: uid}).Decode(&plans); err != nil {
		return err
	}
	plans = list(plans)
	var chats []string
	if err := st.Collection(store.CollThreads).Distinct(ctx, "_id", bson.M{"$or": bson.A{
		bson.M{"itineraryId": bson.M{"$in": plans}}, bson.M{fieldSeed: baselineTag, fieldFor: uid}}}).Decode(&chats); err != nil {
		return err
	}
	chats = list(chats)
	for coll, filter := range map[string]bson.M{
		store.CollMessages:        {"threadId": bson.M{"$in": chats}},
		store.CollExpenses:        {"groupId": bson.M{"$in": chats}},
		store.CollPhotos:          {"groupId": bson.M{"$in": chats}, "kind": models.PhotoKindGroup},
		store.CollItemStates:      {"itineraryId": bson.M{"$in": plans}},
		store.CollRatings:         {"itineraryId": bson.M{"$in": plans}},
		store.CollJoinRequests:    {"$or": bson.A{bson.M{"itineraryId": bson.M{"$in": plans}}, bson.M{"postId": bson.M{"$in": plans}}}},
		store.CollPlanTogether:    {"postId": bson.M{"$in": plans}},
		store.CollCheckoutIntents: {"itineraryId": bson.M{"$in": plans}},
		store.CollCheckoutRuns:    {"itineraryId": bson.M{"$in": plans}},
		store.CollThreads:         {"_id": bson.M{"$in": chats}},
		store.CollItineraries:     {"_id": bson.M{"$in": plans}},
	} {
		if _, err := st.Collection(coll).DeleteMany(ctx, filter); err != nil {
			return fmt.Errorf("clear the default upcoming sidequests from %s: %w", coll, err)
		}
	}
	return nil
}

// writeUpcoming makes the person's default upcoming sidequests again.
func writeUpcoming(ctx context.Context, st *store.Store, uid string, docs []upcomingDoc) error {
	if err := clearUpcoming(ctx, st, uid); err != nil {
		return err
	}
	tagged := func(doc any) (bson.D, error) {
		raw, err := bson.Marshal(doc)
		if err != nil {
			return nil, err
		}
		var d bson.D
		if err := bson.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		return append(d, bson.E{Key: fieldSeed, Value: baselineTag}, bson.E{Key: fieldFor, Value: uid}), nil
	}
	for _, u := range docs {
		inserts := []struct {
			coll string
			doc  any
		}{{store.CollItineraries, u.plan}, {store.CollThreads, u.thread}}
		for _, m := range u.messages {
			inserts = append(inserts, struct {
				coll string
				doc  any
			}{store.CollMessages, m})
		}
		for _, in := range inserts {
			d, err := tagged(in.doc)
			if err != nil {
				return err
			}
			if th, ok := in.doc.(*models.Thread); ok {
				d = memberOrder(d, th)
			}
			if _, err := st.Collection(in.coll).InsertOne(ctx, d); err != nil {
				return fmt.Errorf("write the default upcoming sidequests: %w", err)
			}
		}
	}
	return nil
}

// memberOrder writes a chat's unread and read marks in member order (Go
// maps marshal in random order), so the chat made again is byte for byte
// the same.
func memberOrder(d bson.D, th *models.Thread) bson.D {
	for i, e := range d {
		switch e.Key {
		case "unread":
			m := bson.D{}
			for _, id := range th.MemberIDs {
				if n, ok := th.Unread[id]; ok {
					m = append(m, bson.E{Key: id, Value: n})
				}
			}
			d[i].Value = m
		case "readAt":
			m := bson.D{}
			for _, id := range th.MemberIDs {
				if t, ok := th.ReadAt[id]; ok {
					m = append(m, bson.E{Key: id, Value: t})
				}
			}
			d[i].Value = m
		}
	}
	return d
}

func (p printer) upcoming(docs []upcomingDoc, notes []string, loc *time.Location) {
	p.f("  Default upcoming sidequests (made again with fresh dates on every --apply and --reset): %d", len(docs))
	for _, u := range docs {
		start, end := u.plan.Start.In(loc), u.plan.BackBy.In(loc)
		var stops []string
		for _, item := range u.plan.Items {
			if item.Kind == models.ItemStop {
				stops = append(stops, item.Title)
			}
		}
		p.f("    + %s · %s, %s · %s, host %s, with %s · chat of %d", u.plan.Title, start.Format("Mon Jan 2"), httpx.TimeRange(start, end),
			u.plan.Visibility, u.host, orNobody(u.who), len(u.messages))
		p.f("        %s", strings.Join(stops, " → "))
	}
	for _, n := range notes {
		p.f("  Note: %s", n)
	}
}

func orNobody(names []string) string {
	if len(names) == 0 {
		return "nobody else yet"
	}
	return strings.Join(names, " and ")
}
