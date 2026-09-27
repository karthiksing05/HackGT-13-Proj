package main

import (
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Reset mode (--reset @handle,…) puts a person back to the baseline the
// seeds gave them, so they can practice making sidequests and wipe: their
// history-v1 plans, ratings and interests, their calendar-v1 events and the
// showcase world as seeded. Everything else they have in plans goes:
// sidequests they host that no seed wrote (with all that hangs off them, as
// when a host deletes a plan), their places in other people's plans, their
// joins to seeded plans, their "free now" posts, ratings, notes, tickets and
// checkouts. Their account, friendships and requests, direct messages,
// payment methods and other people's own data are never touched.

// seedOwned reports whether a document came from a seed: tagged by this
// command, or one of the older demo fixture's "seed-…" documents.
func seedOwned(raw bson.Raw) bool {
	if tag, ok := raw.Lookup(fieldSeed).StringValueOK(); ok && tag != "" {
		return true
	}
	id, _ := raw.Lookup("_id").StringValueOK()
	return strings.HasPrefix(id, "seed-")
}

// seededMemberOf reports whether uid belongs to a seeded plan as the seed
// made it: its host, or the demo account where the showcase spec puts her.
func seededMemberOf(it *models.Itinerary, raw bson.Raw, uid string, demo bool) bool {
	if it.HostID == uid {
		return true
	}
	if tag, _ := raw.Lookup(fieldSeed).StringValueOK(); tag != seedTag || !demo {
		return false
	}
	for _, spec := range saltlight.plans {
		if "showcase-slt-plan-"+spec.key == it.ID && slices.Contains(spec.members, sandyKey) {
			return true
		}
	}
	return false
}

// resetPlan is what a reset takes away from one person, and puts back.
type resetPlan struct {
	hp        *histPerson
	demo      bool
	hosted    []*models.Itinerary // their own plans no seed wrote, any status: deleted
	hostedIDs []string
	threads   []string            // the group chats of those plans: deleted
	left      []*models.Itinerary // other people's plans (no seed wrote) they are in: they leave
	rejoined  []*models.Itinerary // seeded plans they joined: back to the seeded members
	stayed    []*models.Itinerary // seeded plans they are in as seeded (the demo account's)
	oldHosted []string            // of those, the older fixture's ("seed-…"): their ratings are its baseline
	seedChats []string            // the chats of the seeded plans they are in or joined
	history   []string            // their history plans (the baseline)
	counts    map[string]int64    // what goes, per collection
	losers    map[string][]string // hosted plan id → the other members who lose it
	calendar  bool                // their calendar-v1 events were written: put them back
}

// resetPeople resolves --reset: like --history, but the demo account is
// allowed with --allow-demo.
func runReset(ctx context.Context, st *store.Store, o Options, p printer) error {
	people, err := resolveAccounts(ctx, st, o.Reset, o.AllowDemo)
	if err != nil {
		return err
	}
	for _, hp := range people {
		if hp.backup, err = loadBackup(ctx, st, hp.uid()); err != nil {
			return err
		}
	}
	if err := pickFlavors(people); err != nil {
		return err
	}
	cat, err := loadCatalog(ctx, st, store.DefaultCatalog)
	if err != nil {
		return err
	}
	crew, err := showcasePeople(ctx, st)
	if err != nil {
		return err
	}
	var plans []*resetPlan
	for _, hp := range people {
		rp, err := planReset(ctx, st, hp, o)
		if err != nil {
			return err
		}
		if hp.backup != nil {
			// The baseline: the history as its first --apply laid it out.
			if err := planHistory(ctx, st, hp, cat, crew, o); err != nil {
				return err
			}
		}
		p.reset(rp, o.Atlanta)
		plans = append(plans, rp)
	}
	p.f("")
	if !o.Apply {
		p.f("Dry run: nothing was deleted or restored. Run again with --reset … --apply.")
		return nil
	}
	for _, rp := range plans {
		if err := rp.apply(ctx, st); err != nil {
			return fmt.Errorf("@%s: %w", rp.hp.user.Username, err)
		}
		p.f("  @%s: back to the baseline", rp.hp.user.Username)
	}
	p.f("Done.")
	return nil
}

func planReset(ctx context.Context, st *store.Store, hp *histPerson, o Options) (*resetPlan, error) {
	uid := hp.uid()
	rp := &resetPlan{hp: hp, demo: store.IsDemoCast(hp.user), counts: map[string]int64{}, losers: map[string][]string{}}
	cursor, err := st.Collection(store.CollItineraries).Find(ctx, bson.M{"$or": bson.A{bson.M{"hostId": uid}, bson.M{"memberIds": uid}}},
		options.Find().SetSort(bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var raws []bson.Raw
	if err := cursor.All(ctx, &raws); err != nil {
		return nil, err
	}
	var others []string
	for _, raw := range raws {
		var it models.Itinerary
		if err := bson.Unmarshal(raw, &it); err != nil {
			return nil, err
		}
		switch {
		case !seedOwned(raw) && it.HostID == uid:
			rp.hosted = append(rp.hosted, &it)
			rp.hostedIDs = append(rp.hostedIDs, it.ID)
			for _, m := range it.MemberIDs {
				if m != uid {
					others = append(others, m)
				}
			}
		case !seedOwned(raw):
			rp.left = append(rp.left, &it)
		case strings.HasPrefix(it.ID, "history-"+uid+"-"):
			rp.history = append(rp.history, it.ID)
		case !seededMemberOf(&it, raw, uid, rp.demo):
			rp.rejoined = append(rp.rejoined, &it)
		default:
			rp.stayed = append(rp.stayed, &it)
			if strings.HasPrefix(it.ID, "seed-") {
				rp.oldHosted = append(rp.oldHosted, it.ID)
			}
		}
	}
	names, err := st.Users().ByIDs(ctx, others)
	if err != nil {
		return nil, err
	}
	for _, it := range rp.hosted {
		for _, m := range it.MemberIDs {
			if m == uid {
				continue
			}
			name := "a deleted account"
			if u := names[m]; u != nil {
				name = u.Name
			}
			rp.losers[it.ID] = append(rp.losers[it.ID], name)
		}
	}
	if rp.threads, err = chatsOf(ctx, st, rp.hosted); err != nil {
		return nil, err
	}
	if rp.seedChats, err = chatsOf(ctx, st, append(slices.Clone(rp.rejoined), rp.stayed...)); err != nil {
		return nil, err
	}
	marker, err := st.Collection(collBackups).CountDocuments(ctx, bson.M{"_id": calendarMarkerID(uid)})
	if err != nil {
		return nil, err
	}
	rp.calendar = marker > 0
	for coll, filter := range rp.filters() {
		n, err := st.Collection(coll).CountDocuments(ctx, filter)
		if err != nil {
			return nil, err
		}
		rp.counts[coll] = n
	}
	return rp, nil
}

// chatsOf are the group chats of plans: the ones pointing at them and the
// ones they point at.
func chatsOf(ctx context.Context, st *store.Store, plans []*models.Itinerary) ([]string, error) {
	var ids, own []string
	for _, it := range plans {
		ids = append(ids, it.ID)
		if it.ThreadID != "" {
			own = append(own, it.ThreadID)
		}
	}
	var out []string
	if err := st.Collection(store.CollThreads).Distinct(ctx, "_id", bson.M{"$or": bson.A{
		bson.M{"itineraryId": bson.M{"$in": list(ids)}}, bson.M{"_id": bson.M{"$in": list(own)}}}}).Decode(&out); err != nil {
		return nil, err
	}
	return list(out), nil
}

// filters are the deletions of a reset, per collection (several filters
// for one collection are combined with $or).
func (rp *resetPlan) filters() map[string]bson.M {
	uid := rp.hp.uid()
	hosted, chats, seedChats := list(rp.hostedIDs), list(rp.threads), list(rp.seedChats)
	keepRatings := bson.A{bson.M{fieldSeed: historyTag, fieldFor: uid}}
	for _, it := range rp.oldHosted {
		keepRatings = append(keepRatings, bson.M{"itineraryId": it})
	}
	or := func(fs ...bson.M) bson.M { return bson.M{"$or": bson.A(toAny(fs))} }
	// theirs are the person's own documents in seeded chats: not written by a seed.
	theirs := func(f bson.M) bson.M {
		f[fieldSeed] = bson.M{"$exists": false}
		f["_id"] = bson.M{"$not": bson.Regex{Pattern: "^seed-"}}
		return f
	}
	return map[string]bson.M{
		store.CollItineraries: {"_id": bson.M{"$in": hosted}},
		store.CollThreads:     {"_id": bson.M{"$in": chats}},
		store.CollMessages:    or(bson.M{"threadId": bson.M{"$in": chats}}, theirs(bson.M{"threadId": bson.M{"$in": seedChats}, "senderId": uid})),
		store.CollExpenses:    or(bson.M{"groupId": bson.M{"$in": chats}}, theirs(bson.M{"groupId": bson.M{"$in": seedChats}, "createdBy": uid})),
		store.CollPhotos: or(bson.M{"groupId": bson.M{"$in": chats}, "kind": models.PhotoKindGroup},
			theirs(bson.M{"groupId": bson.M{"$in": seedChats}, "ownerId": uid, "kind": models.PhotoKindGroup})),
		store.CollItemStates: or(bson.M{"itineraryId": bson.M{"$in": hosted}}, bson.M{"userId": uid}),
		store.CollRatings:    or(bson.M{"itineraryId": bson.M{"$in": hosted}}, bson.M{"userId": uid, "$nor": keepRatings}),
		store.CollJoinRequests: or(bson.M{"itineraryId": bson.M{"$in": hosted}}, bson.M{"postId": bson.M{"$in": hosted}},
			bson.M{"userId": uid}),
		store.CollPlanTogether: or(bson.M{"postId": bson.M{"$in": hosted}},
			bson.M{"userId": uid, "postId": bson.M{"$regex": "^(showcase-|seed-)"}}),
		store.CollCheckoutIntents: or(bson.M{"itineraryId": bson.M{"$in": hosted}}, bson.M{"userId": uid}),
		store.CollCheckoutRuns:    or(bson.M{"itineraryId": bson.M{"$in": hosted}}, bson.M{"userId": uid}),
		store.CollForumPosts:      {"authorId": uid, "type": models.PostFreeNow},
		store.CollCalendarEvents:  {"userId": uid, fieldSeed: bson.M{"$ne": calendarTag}},
	}
}

func toAny(fs []bson.M) []any {
	out := make([]any, len(fs))
	for i, f := range fs {
		out[i] = f
	}
	return out
}

// ---- report ------------------------------------------------------------------

func (p printer) reset(rp *resetPlan, loc *time.Location) {
	u := rp.hp.user
	p.f("")
	p.f("@%s · %s · id %s", u.Username, u.Name, u.ID.Hex())
	p.f("  Their own sidequests (no seed wrote them), deleted with everything on them: %d", len(rp.hosted))
	for _, it := range rp.hosted {
		line := fmt.Sprintf("    - “%s” · %s · %s", it.Title, planWhen(it, loc), it.Status)
		if l := rp.losers[it.ID]; len(l) == 1 {
			line += " · " + l[0] + " loses it"
		} else if len(l) > 1 {
			line += " · " + strings.Join(l, ", ") + " lose it"
		}
		p.f("%s", line)
	}
	p.f("  Other people's plans they are in, left: %d", len(rp.left))
	for _, it := range rp.left {
		p.f("    - “%s” · %s", it.Title, planWhen(it, loc))
	}
	p.f("  Seeded plans they joined, back to the seeded members: %d", len(rp.rejoined))
	for _, it := range rp.rejoined {
		p.f("    - “%s” · %s", it.Title, planWhen(it, loc))
	}
	var parts []string
	for _, c := range []struct{ coll, what string }{
		{store.CollThreads, "group chats"}, {store.CollMessages, "messages"}, {store.CollExpenses, "expenses"},
		{store.CollPhotos, "group photos"}, {store.CollItemStates, "notes and tickets"}, {store.CollRatings, "ratings"},
		{store.CollJoinRequests, "join records"}, {store.CollPlanTogether, "“Plan together” marks"},
		{store.CollCheckoutIntents, "checkouts"}, {store.CollCheckoutRuns, "Muse runs"}, {store.CollForumPosts, "“free now” posts"},
		{store.CollCalendarEvents, "calendar events not from the seed"},
	} {
		if n := rp.counts[c.coll]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, c.what))
		}
	}
	if len(parts) == 0 {
		parts = []string{"nothing"}
	}
	p.f("  Also deleted: %s", strings.Join(parts, ", "))
	var back []string
	if rp.hp.backup != nil {
		back = append(back, fmt.Sprintf("%d history plans (%s, dates from %s) with their %d ratings", len(rp.hp.plans), rp.hp.flavor.key,
			rp.hp.anchor.In(loc).Format("Jan 2"), len(rp.hp.ratings)), "interests, taste and taste vectors as the history seed left them")
	} else {
		back = append(back, "no history (none was seeded for them)")
	}
	if rp.calendar {
		back = append(back, fmt.Sprintf("their %s calendar", rp.hp.flavor.key))
	}
	p.f("  Back to the baseline: %s", strings.Join(back, "; "))
	p.f("  Never touched: their account, friendships and requests, direct messages, payment methods, other people's own data")
}

func planWhen(it *models.Itinerary, loc *time.Location) string {
	if it.Start.IsZero() {
		return "no date"
	}
	start, end := it.Start.In(loc), it.BackBy.In(loc)
	return start.Format("Mon Jan 2") + ", " + httpx.TimeRange(start, end)
}

// ---- apply ---------------------------------------------------------------------

func (rp *resetPlan) apply(ctx context.Context, st *store.Store) error {
	uid := rp.hp.uid()
	// Leave other people's plans and their chats, as the app's Leave does.
	for _, it := range rp.left {
		if _, err := st.Collection(store.CollItineraries).UpdateOne(ctx, bson.M{"_id": it.ID},
			bson.M{"$pull": bson.M{"memberIds": uid}}); err != nil {
			return err
		}
	}
	leftChats, err := chatsOf(ctx, st, rp.left)
	if err != nil {
		return err
	}
	if err := pullFromChats(ctx, st, leftChats, uid); err != nil {
		return err
	}
	// Seeded plans they joined: back to the seeded members and times.
	for _, it := range rp.rejoined {
		if _, err := st.Collection(store.CollItineraries).UpdateOne(ctx, bson.M{"_id": it.ID},
			bson.M{"$pull": bson.M{"memberIds": uid}, "$set": bson.M{"updatedAt": it.CreatedAt}}); err != nil {
			return err
		}
	}
	// The deletions, then the seeded chats' last message (their own is gone).
	for coll, filter := range rp.filters() {
		if _, err := st.Collection(coll).DeleteMany(ctx, filter); err != nil {
			return fmt.Errorf("delete from %s: %w", coll, err)
		}
	}
	rejoinedChats, err := chatsOf(ctx, st, rp.rejoined)
	if err != nil {
		return err
	}
	if err := pullFromChats(ctx, st, rejoinedChats, uid); err != nil {
		return err
	}
	if err := touchSeedChats(ctx, st, rp.seedChats); err != nil {
		return err
	}
	if err := rp.restoreUnread(ctx, st); err != nil {
		return err
	}
	// Shared notes they wrote on seeded plans they stay in or left.
	if _, err := st.Collection(store.CollItineraries).UpdateMany(ctx, bson.M{"items.sharedNotesBy": uid, "_id": bson.M{"$nin": list(rp.hostedIDs)}},
		bson.M{"$unset": bson.M{"items.$[n].sharedNotes": "", "items.$[n].sharedNotesBy": ""}},
		options.UpdateMany().SetArrayFilters([]any{bson.M{"n.sharedNotesBy": uid}})); err != nil {
		return fmt.Errorf("clear shared notes: %w", err)
	}
	// The baseline: the history's plans and ratings as seeded, the account's
	// interests, taste and vectors as the history left them, the calendar.
	if rp.hp.backup != nil {
		if err := rp.restoreHistory(ctx, st); err != nil {
			return err
		}
	}
	if rp.calendar {
		if err := writeCalendar(ctx, st, uid, calendarEvents(uid, schedules[rp.hp.flavor.key])); err != nil {
			return err
		}
	}
	return nil
}

// pullFromChats takes uid out of chats: member, unread and read marks
// (a seeded chat's last message and updatedAt are set by touchSeedChats).
func pullFromChats(ctx context.Context, st *store.Store, chats []string, uid string) error {
	for _, id := range chats {
		update := bson.M{"$pull": bson.M{"memberIds": uid}, "$unset": bson.M{"unread." + uid: "", "readAt." + uid: ""}}
		if _, err := st.Collection(store.CollThreads).UpdateOne(ctx, bson.M{"_id": id}, update); err != nil {
			return err
		}
	}
	return nil
}

// touchSeedChats points each seeded chat's last message at the newest one
// left and puts updatedAt back to it, as the seed wrote them, and gives
// every member the unread count the app keeps (the messages from others
// since they last read), which the deleted messages had raised.
func touchSeedChats(ctx context.Context, st *store.Store, chats []string) error {
	messages := st.Collection(store.CollMessages)
	for _, id := range chats {
		var last models.Message
		err := messages.FindOne(ctx, bson.M{"threadId": id},
			options.FindOne().SetSort(bson.D{{Key: "sentAt", Value: -1}, {Key: "_id", Value: -1}})).Decode(&last)
		if errors.Is(err, mongo.ErrNoDocuments) {
			continue
		}
		if err != nil {
			return err
		}
		set := bson.M{"lastMessageText": last.Text, "lastSenderId": last.SenderID, "lastMessageAt": last.SentAt, "updatedAt": last.SentAt}
		var th models.Thread
		if err := st.Collection(store.CollThreads).FindOne(ctx, bson.M{"_id": id}).Decode(&th); err != nil {
			return err
		}
		for _, member := range th.MemberIDs {
			readAt, ok := th.ReadAt[member]
			if !ok {
				continue
			}
			n, err := messages.CountDocuments(ctx, bson.M{"threadId": id, "sentAt": bson.M{"$gt": readAt}, "senderId": bson.M{"$ne": member}})
			if err != nil {
				return err
			}
			set["unread."+member] = int(n)
		}
		if _, err := st.Collection(store.CollThreads).UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set}); err != nil {
			return err
		}
	}
	return nil
}

// restoreHistory rewrites the history's plans as laid out from the backup
// (same story and dates), puts each baseline rating back to its stars, tags
// and note (inserting one that is gone), and sets the account's fields to
// what the last history --apply left.
func (rp *resetPlan) restoreHistory(ctx context.Context, st *store.Store) error {
	hp := rp.hp
	uid := hp.uid()
	for _, pl := range hp.plans {
		d, err := historyDoc(pl.doc, uid)
		if err != nil {
			return err
		}
		if _, err := st.Collection(store.CollItineraries).ReplaceOne(ctx, bson.M{"_id": pl.doc.ID, fieldSeed: historyTag}, d,
			options.Replace().SetUpsert(true)); err != nil {
			return fmt.Errorf("write %s: %w", pl.doc.ID, err)
		}
	}
	activity := map[string]string{}
	backBy := map[string]time.Time{}
	for _, pl := range hp.plans {
		for _, item := range pl.doc.Items {
			activity[item.ID] = item.ActivityID
			backBy[item.ID] = pl.doc.BackBy
		}
	}
	for _, r := range hp.ratings {
		set := bson.M{"stars": r.stars, "tags": list(r.tags), "itineraryId": r.planID, "activityId": activity[r.itemID]}
		update := bson.M{"$set": set}
		if r.note != "" {
			set["note"] = r.note
		} else {
			update["$unset"] = bson.M{"note": ""}
		}
		res, err := st.Collection(store.CollRatings).UpdateOne(ctx, bson.M{"userId": uid, "itemId": r.itemID, fieldSeed: historyTag}, update)
		if err != nil {
			return err
		}
		if res.MatchedCount > 0 {
			continue
		}
		at := backBy[r.itemID].Add(2 * time.Hour)
		rating := models.Rating{ID: models.PairID(uid, r.itemID), UserID: uid, ItemID: r.itemID, ItineraryID: r.planID,
			ActivityID: activity[r.itemID], Stars: r.stars, Tags: list(r.tags), CreatedAt: at, UpdatedAt: at}
		if r.note != "" {
			note := r.note
			rating.Note = &note
		}
		d, err := historyDoc(&rating, uid)
		if err != nil {
			return err
		}
		if _, err := st.Collection(store.CollRatings).InsertOne(ctx, d); err != nil {
			return fmt.Errorf("put back the rating of %s: %w", r.itemID, err)
		}
	}
	b := hp.backup
	if len(b.Seeded) == 0 {
		return nil
	}
	seeded, err := b.Seeded.Elements()
	if err != nil {
		return err
	}
	set := bson.M{}
	for _, e := range seeded {
		set[e.Key()] = e.Value()
	}
	update := bson.M{"$set": set}
	if len(b.SeededMissing) > 0 {
		unset := bson.M{}
		for _, name := range b.SeededMissing {
			unset[name] = ""
		}
		update["$unset"] = unset
	}
	_, err = st.Collection(store.CollUsers).UpdateOne(ctx, bson.M{"_id": hp.user.ID}, update)
	return err
}

// restoreUnread gives the demo account the unread count its seeded chats
// had (the showcase spec's), read up to the message before them.
func (rp *resetPlan) restoreUnread(ctx context.Context, st *store.Store) error {
	uid := rp.hp.uid()
	for _, it := range rp.stayed {
		for _, spec := range saltlight.plans {
			if "showcase-slt-plan-"+spec.key != it.ID {
				continue
			}
			unread := spec.unread[sandyKey]
			chatID := "showcase-slt-chat-" + spec.key
			var msgs []models.Message
			cursor, err := st.Collection(store.CollMessages).Find(ctx, bson.M{"threadId": chatID, fieldSeed: seedTag},
				options.Find().SetSort(bson.D{{Key: "sentAt", Value: 1}, {Key: "_id", Value: 1}}))
			if err != nil {
				return err
			}
			if err := cursor.All(ctx, &msgs); err != nil {
				return err
			}
			var th models.Thread
			if err := st.Collection(store.CollThreads).FindOne(ctx, bson.M{"_id": chatID}).Decode(&th); err != nil {
				return err
			}
			unread = min(unread, len(msgs))
			readAt := th.LastMessageAt
			if unread > 0 {
				readAt = th.CreatedAt
				if k := len(msgs) - unread - 1; k >= 0 {
					readAt = msgs[k].SentAt
				}
			}
			if _, err := st.Collection(store.CollThreads).UpdateOne(ctx, bson.M{"_id": chatID},
				bson.M{"$set": bson.M{"unread." + uid: unread, "readAt." + uid: readAt}}); err != nil {
				return err
			}
		}
	}
	return nil
}

func calendarMarkerID(uid string) string { return calendarTag + "|" + uid }
