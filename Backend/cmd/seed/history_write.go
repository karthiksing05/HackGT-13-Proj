package main

import (
	"Backend/pkg/api"
	"Backend/pkg/api/itineraries"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/ml"
	"Backend/pkg/profiles"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// runHistory is --history: plan (and with --apply write, or with --remove
// take back) the history of each person.
func runHistory(ctx context.Context, st *store.Store, o Options, p printer) error {
	people, err := resolveHistory(ctx, st, o.History)
	if err != nil {
		return err
	}
	if o.Remove {
		return removeHistory(ctx, st, o, p, people)
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
	taste := checkTaste(ctx, o.ML, o.MLURL)
	p.f("Taste vectors: %s", taste.line)
	var vectors []catalogVector
	if !taste.usable {
		if vectors, err = loadVectors(ctx, st, store.DefaultCatalog); err != nil {
			return err
		}
	}
	conflicts := 0
	for _, hp := range people {
		if err := planHistory(ctx, st, hp, cat, crew, o); err != nil {
			return err
		}
		if taste.usable {
			hp.taste = "rebuilt by the ML service from their preferences and ratings (/v1/user-profile)"
		} else {
			var n int
			hp.pos, n = blend(hp.likes, vectors, true)
			hp.neg, _ = blend(hp.likes, vectors, false)
			hp.taste = fmt.Sprintf("blended from %d catalog items of what they like", n)
			if hp.pos == nil {
				hp.taste = "left as they are (the catalog has no vectors for what they like)"
			}
		}
		p.history(hp, taste, o.Atlanta)
		hp.upcoming, hp.upNotes = planUpcoming(hp, cat, crew, o.Now, o.Atlanta)
		p.upcoming(hp.upcoming, hp.upNotes, o.Atlanta)
		for _, pl := range hp.plans {
			if pl.act == actConflict {
				conflicts++
			}
		}
	}
	p.f("")
	if !o.Apply {
		p.f("Dry run: nothing was written. Run again with --apply to write it.")
		return nil
	}
	if conflicts > 0 {
		return fmt.Errorf("refusing to write: %d past plan id(s) belong to documents that are not this seed's; nothing was written", conflicts)
	}
	p.f("Writing to %s", o.Target)
	rater := newRater(st)
	for _, hp := range people {
		if err := hp.apply(ctx, st, rater, taste); err != nil {
			return fmt.Errorf("@%s: %w", hp.user.Username, err)
		}
		if err := writeUpcoming(ctx, st, hp.uid(), hp.upcoming); err != nil {
			return fmt.Errorf("@%s: %w", hp.user.Username, err)
		}
		p.f("  @%s: %d past sidequests, %d ratings, interests %s, taste vectors %s; %d default upcoming sidequests", hp.user.Username,
			len(hp.plans), len(hp.ratings), orNone(hp.changed), hp.taste, len(hp.upcoming))
	}
	p.f("Done. --history … --remove --apply takes it back out and restores what it changed.")
	return nil
}

func orNone(keys []string) string {
	if len(keys) == 0 {
		return "unchanged"
	}
	return "+" + strings.Join(keys, ", +")
}

// historyDoc is doc as the API stores it, tagged with the history and whose it is.
func historyDoc(doc any, uid string) (bson.D, error) {
	raw, err := bson.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var d bson.D
	if err := bson.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return append(d, bson.E{Key: fieldSeed, Value: historyTag}, bson.E{Key: fieldFor, Value: uid}), nil
}

// fieldsOf are the named fields of an account as stored, and the names of
// the ones it does not have.
func fieldsOf(raw bson.Raw, names []string) (bson.D, []string) {
	var present bson.D
	var missing []string
	for _, name := range names {
		if v, err := raw.LookupErr(name); err == nil {
			present = append(present, bson.E{Key: name, Value: v})
		} else {
			missing = append(missing, name)
		}
	}
	return present, list(missing)
}

// apply writes one person's history: the originals first (once), then the
// plans, the ratings, the merged preferences and the taste vectors, and
// last what the account looks like now, which --remove compares with.
func (hp *histPerson) apply(ctx context.Context, st *store.Store, rater *historyRater, taste tasteSource) error {
	users := st.Collection(store.CollUsers)
	uid := hp.uid()
	if hp.backup == nil {
		original, missing := fieldsOf(hp.raw, historyFields)
		raw, err := bson.Marshal(original)
		if err != nil {
			return err
		}
		b := backup{ID: backupID(uid), Seed: historyTag, For: uid, Username: hp.user.Username, Flavor: hp.flavor.key,
			SavedAt: time.Now().UTC(), Anchor: hp.anchor.UTC(), Original: raw, Missing: missing}
		if _, err := st.Collection(collBackups).InsertOne(ctx, b); err != nil {
			return fmt.Errorf("save the originals: %w", err)
		}
		hp.backup = &b
	}
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
	for _, r := range hp.ratings {
		if err := rater.rate(uid, r); err != nil {
			return err
		}
		if _, err := st.Collection(store.CollRatings).UpdateOne(ctx, bson.M{"userId": uid, "itemId": r.itemID},
			bson.M{"$set": bson.M{fieldSeed: historyTag, fieldFor: uid}}); err != nil {
			return fmt.Errorf("tag the rating of %s: %w", r.itemID, err)
		}
	}
	if len(hp.changed) > 0 {
		if _, err := users.UpdateOne(ctx, bson.M{"_id": hp.user.ID},
			bson.M{"$set": bson.M{"prefs.ratings": hp.likes, "updatedAt": time.Now().UTC()}}); err != nil {
			return fmt.Errorf("merge the preferences: %w", err)
		}
	}
	if err := hp.writeTaste(ctx, st, taste); err != nil {
		return err
	}
	now, err := users.FindOne(ctx, bson.M{"_id": hp.user.ID}).Raw()
	if err != nil {
		return err
	}
	seeded, missing := fieldsOf(now, historyFields)
	raw, err := bson.Marshal(seeded)
	if err != nil {
		return err
	}
	_, err = st.Collection(collBackups).UpdateOne(ctx, bson.M{"_id": backupID(uid)}, bson.M{
		"$set":      bson.M{"seeded": bson.Raw(raw), "seededMissing": missing},
		"$addToSet": bson.M{"ratingsChanged": bson.M{"$each": list(hp.changed)}},
	})
	return err
}

// writeTaste rebuilds the person's taste vectors from their preferences and
// ratings through the ML service (pkg/profiles, as for any account), or
// writes the blend. A blend empties the profile hashes, so the next real
// refresh rebuilds them through the service; it sets rather than removes
// fields, so --remove puts the document back byte for byte (a field
// removed and added back would move to its end).
func (hp *histPerson) writeTaste(ctx context.Context, st *store.Store, taste tasteSource) error {
	if taste.usable {
		rctx, cancel := context.WithTimeout(ctx, profileTimeout)
		err := profiles.New(st, taste.client, time.Now).Refresh(rctx, hp.uid())
		cancel()
		if err == nil {
			return nil
		}
		hp.taste = fmt.Sprintf("not rebuilt: the ML service failed (%v)", err)
		return nil
	}
	if hp.pos == nil {
		return nil
	}
	neg := hp.neg
	if neg == nil {
		neg = make([]float64, ml.Dim) // all zeros: no dislikes
	}
	_, err := st.Collection(store.CollUsers).UpdateOne(ctx, bson.M{"_id": hp.user.ID}, bson.M{"$set": bson.M{
		"positiveEmbedding": hp.pos, "negativeEmbedding": neg, "embeddingModel": ml.Model,
		"profileTextHash": "", "profileInputHash": "",
	}})
	return err
}

// historyRater runs the app's rating handler (PUT /ratings/{itemId},
// pkg/api/itineraries) in this process, so each rating, the taste tags it
// moves and the ratings count come out exactly as when the person rates in
// the app. Its token is signed with a secret that lives only in this
// process; nothing is stored for it.
type historyRater struct {
	router http.Handler
	secret string
}

func newRater(st *store.Store) *historyRater {
	secret := util.RandomToken(32)
	deps := &api.Deps{Store: st, Cfg: &config.Config{JWTSecret: secret, PublicBaseURL: "http://seed.invalid"}, Now: time.Now}
	r := mux.NewRouter()
	itineraries.Register(r, deps)
	return &historyRater{router: r, secret: secret}
}

func (hr *historyRater) rate(uid string, r histRating) error {
	token, _, _, err := util.SignToken(hr.secret, util.TokenTypeAccess, uid, time.Now(), time.Hour)
	if err != nil {
		return err
	}
	body := contract.Rating{Stars: r.stars, Tags: list(r.tags)}
	if r.note != "" {
		note := r.note
		body.Note = &note
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req := httptest.NewRequest(http.MethodPut, "/ratings/"+url.PathEscape(r.itemID), bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Time-Zone", atlantaTZ)
	rec := httptest.NewRecorder()
	hr.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		return fmt.Errorf("rate %s: HTTP %d %s", r.itemID, rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	return nil
}

// ---- remove ----------------------------------------------------------------------

// removeHistory deletes each person's history (the plans, every rating of
// their stops, the notes on them) and puts back the fields it changed.
func removeHistory(ctx context.Context, st *store.Store, o Options, p printer, people []*histPerson) error {
	type job struct {
		hp      *histPerson
		planIDs []any
		filters map[string]bson.M
		update  bson.M
	}
	var jobs []job
	for _, hp := range people {
		uid := hp.uid()
		var planIDs []any
		if err := st.Collection(store.CollItineraries).Distinct(ctx, "_id", bson.M{fieldSeed: historyTag, fieldFor: uid}).Decode(&planIDs); err != nil {
			return fmt.Errorf("read @%s's past plans: %w", hp.user.Username, err)
		}
		planIDs = list(planIDs)
		filters := map[string]bson.M{
			store.CollRatings:     {"$or": bson.A{bson.M{"itineraryId": bson.M{"$in": planIDs}}, bson.M{fieldSeed: historyTag, fieldFor: uid}}},
			store.CollItemStates:  {"itineraryId": bson.M{"$in": planIDs}},
			store.CollItineraries: {fieldSeed: historyTag, fieldFor: uid},
		}
		p.f("")
		p.f("@%s · %s · id %s", hp.user.Username, hp.user.Name, uid)
		for _, coll := range []string{store.CollItineraries, store.CollRatings, store.CollItemStates} {
			n, err := st.Collection(coll).CountDocuments(ctx, filters[coll])
			if err != nil {
				return err
			}
			p.f("  %s: %d", coll, n)
		}
		up, err := st.Collection(store.CollItineraries).CountDocuments(ctx, bson.M{fieldSeed: baselineTag, fieldFor: uid})
		if err != nil {
			return err
		}
		p.f("  default upcoming sidequests (with their chats): %d", up)
		b, err := loadBackup(ctx, st, uid)
		if err != nil {
			return err
		}
		var update bson.M
		if b == nil {
			p.f("  Originals: none saved (nothing of theirs was changed)")
		} else {
			var lines []string
			update, lines = restorePlan(hp.raw, b)
			for _, l := range lines {
				p.f("  %s", l)
			}
		}
		jobs = append(jobs, job{hp: hp, planIDs: planIDs, filters: filters, update: update})
	}
	p.f("")
	if !o.Apply {
		p.f("Dry run: nothing was deleted or restored. Run again with --remove --apply.")
		return nil
	}
	for _, j := range jobs {
		for _, coll := range []string{store.CollRatings, store.CollItemStates, store.CollItineraries} {
			if _, err := st.Collection(coll).DeleteMany(ctx, j.filters[coll]); err != nil {
				return fmt.Errorf("@%s: delete from %s: %w", j.hp.user.Username, coll, err)
			}
		}
		if len(j.update) > 0 {
			if _, err := st.Collection(store.CollUsers).UpdateOne(ctx, bson.M{"_id": j.hp.user.ID}, j.update); err != nil {
				return fmt.Errorf("@%s: restore: %w", j.hp.user.Username, err)
			}
		}
		if err := clearUpcoming(ctx, st, j.hp.uid()); err != nil {
			return fmt.Errorf("@%s: %w", j.hp.user.Username, err)
		}
		if _, err := st.Collection(collBackups).DeleteOne(ctx, bson.M{"_id": backupID(j.hp.uid())}); err != nil {
			return fmt.Errorf("@%s: delete the backup: %w", j.hp.user.Username, err)
		}
	}
	p.f("Removed and restored.")
	return nil
}

// value is a field of a stored document, and whether it is there.
func value(raw bson.Raw, missing []string, name string) (bson.RawValue, bool) {
	if raw == nil || slices.Contains(missing, name) {
		return bson.RawValue{}, false
	}
	v, err := raw.LookupErr(strings.Split(name, ".")...)
	return v, err == nil
}

func sameValue(a bson.RawValue, aok bool, b bson.RawValue, bok bool) bool {
	if aok != bok {
		return false
	}
	return !aok || (a.Type == b.Type && bytes.Equal(a.Value, b.Value))
}

// restorePlan is the update that puts a person's changed fields back: a
// field still as the last --apply left it gets its original value (or is
// removed when it had none); one changed since (the person saved
// preferences, rated in the app) is left, except that the preference keys
// this seed filled or raised go back when they still hold its value.
func restorePlan(cur bson.Raw, b *backup) (bson.M, []string) {
	set, unset := bson.M{}, bson.M{}
	var lines []string
	unfinished := len(b.Seeded) == 0 // an --apply that stopped before it finished: restore everything
	unchanged := func(name string) bool {
		c, cok := value(cur, nil, name)
		s, sok := value(b.Seeded, b.SeededMissing, name)
		return unfinished || sameValue(c, cok, s, sok)
	}
	restore := func(name string) {
		if v, ok := value(b.Original, b.Missing, name); ok {
			set[name] = v
		} else {
			unset[name] = ""
		}
	}
	if unchanged("prefs") {
		restore("prefs")
		lines = append(lines, "prefs: restored")
	} else {
		var back, kept []string
		for _, key := range b.RatingsChanged {
			path := "prefs.ratings." + key
			if !unchanged(path) {
				kept = append(kept, key)
				continue
			}
			restore(path)
			back = append(back, key)
		}
		lines = append(lines, fmt.Sprintf("prefs: changed since the seed; its keys put back: %s; changed by them, kept: %s",
			orDash(back), orDash(kept)))
	}
	if unchanged("taste") {
		restore("taste")
		lines = append(lines, "taste tags: restored")
	} else {
		lines = append(lines, "taste tags: changed since the seed (a rating in the app); left as they are")
	}
	vectorsSame := true
	for _, name := range vectorFields {
		vectorsSame = vectorsSame && unchanged(name)
	}
	if vectorsSame {
		for _, name := range vectorFields {
			restore(name)
		}
		lines = append(lines, "taste vectors: restored")
	} else {
		lines = append(lines, "taste vectors: changed since the seed; left as they are")
	}
	if unchanged("updatedAt") {
		restore("updatedAt")
	}
	update := bson.M{}
	if len(set) > 0 {
		update["$set"] = set
	}
	if len(unset) > 0 {
		update["$unset"] = unset
	}
	return update, lines
}

func orDash(keys []string) string {
	if len(keys) == 0 {
		return "none"
	}
	return strings.Join(keys, ", ")
}
