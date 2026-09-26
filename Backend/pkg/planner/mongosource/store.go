// Package mongosource is the planner's MongoDB side: the guaranteed
// pre-filter queries over a catalog collection (planner.md §4.1), the
// phase-B vector fetch for the survivors, and the plan_pools / plan_runs
// documents (§5.6, §6.1). One Store implements planner.CandidateSource,
// EmbeddingSource, ActivityLookup and PoolStore over a *mongo.Database.
package mongosource

import (
	"Backend/pkg/models"
	"Backend/pkg/planner"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Collections the planner owns.
const (
	PoolsCollection = "plan_pools"
	RunsCollection  = "plan_runs"
)

// Index names shared with store.EnsureIndexes: MongoDB rejects the same
// keys under a second name, so both sides must use these.
const (
	ExpiresIndex    = "expiresAt_ttl"
	RunsByUserIndex = "userId_createdAt"
	// StopIDIndex (plan_pools, the planner's own) serves FindStop.
	StopIDIndex = "options_stops_id"
)

const (
	// earthRadiusKm converts the search radius to radians on the same
	// sphere travel.HaversineKm uses, so Mongo's circle and the Go
	// re-check agree.
	earthRadiusKm = 6371.0
	// fetchBatch bounds the $in list of one vector fetch.
	fetchBatch = 500
)

// ErrUnknownCatalog means the catalog name is not on the allow-list, so it
// is never used as a collection name.
var ErrUnknownCatalog = errors.New("mongosource: unknown catalog")

// Store reads the catalogs and keeps pools and runs in one database.
type Store struct {
	db    *mongo.Database
	clock planner.Clock
}

// New wraps a database. clock decides when a pool counts as expired (the
// TTL monitor only sweeps every minute); nil means the system clock.
func New(db *mongo.Database, clock planner.Clock) *Store {
	if clock == nil {
		clock = planner.SystemClock{}
	}
	return &Store{db: db, clock: clock}
}

var (
	_ planner.CandidateSource = (*Store)(nil)
	_ planner.EmbeddingSource = (*Store)(nil)
	_ planner.TextSource      = (*Store)(nil)
	_ planner.ActivityLookup  = (*Store)(nil)
	_ planner.PoolStore       = (*Store)(nil)
	_ planner.StopFinder      = (*Store)(nil)
)

func (s *Store) catalog(name string) (*mongo.Collection, error) {
	c, ok := planner.NormalizeCatalog(name)
	if !ok || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w %q", ErrUnknownCatalog, name)
	}
	return s.db.Collection(c), nil
}

// candidateProjection leaves out what the planner never reads: vectors
// (fetched later for survivors only), trail geometry, long text.
var candidateProjection = bson.D{
	{Key: "embedding", Value: 0}, {Key: "trail.geometry", Value: 0}, {Key: "description", Value: 0},
	{Key: "embeddingText", Value: 0}, {Key: "embeddingMeta", Value: 0}, {Key: "embeddingTextMeta", Value: 0},
	{Key: "sources", Value: 0}, {Key: "sourceKeys", Value: 0}, {Key: "recurrence", Value: 0},
}

// FindCandidates runs the event and place queries in parallel: events
// sorted by start, places by rating then popularity (ties by _id).
func (s *Store) FindCandidates(ctx context.Context, q planner.CandidateQuery) (events, places []models.Activity, err error) {
	coll, err := s.catalog(q.Catalog)
	if err != nil {
		return nil, nil, err
	}
	var (
		wg           sync.WaitGroup
		evErr, plErr error
	)
	if wants(q.Kinds, "event") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			events, evErr = find(ctx, coll, eventFilter(q), bson.D{{Key: "start", Value: 1}, {Key: "_id", Value: 1}}, q.LimitEvents)
		}()
	}
	if wants(q.Kinds, "place") {
		wg.Add(1)
		go func() {
			defer wg.Done()
			places, plErr = find(ctx, coll, placeFilter(q), bson.D{{Key: "rating", Value: -1}, {Key: "popularity", Value: -1}, {Key: "_id", Value: 1}}, q.LimitPlaces)
		}()
	}
	wg.Wait()
	if err := errors.Join(evErr, plErr); err != nil {
		return nil, nil, fmt.Errorf("mongosource: find candidates: %w", err)
	}
	return events, places, nil
}

func wants(kinds []string, kind string) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

func find(ctx context.Context, coll *mongo.Collection, filter, sort bson.D, limit int) ([]models.Activity, error) {
	opts := options.Find().SetProjection(candidateProjection).SetSort(sort)
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cur, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var out []models.Activity
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// eventFilter is §4.1's event query: fixed starts inside [From, To−15m],
// drop-ins overlapping the window, plus the shared conditions.
func eventFilter(q planner.CandidateQuery) bson.D {
	f, and := shared(q, "event")
	late := q.To.Add(-planner.EventStartMargin)
	and = append(and, bson.D{{Key: "$or", Value: bson.A{
		bson.D{
			{Key: "attendance", Value: bson.D{{Key: "$ne", Value: "drop_in"}}},
			{Key: "start", Value: bson.D{{Key: "$gte", Value: q.From}, {Key: "$lte", Value: late}}},
		},
		bson.D{
			{Key: "attendance", Value: "drop_in"},
			{Key: "start", Value: bson.D{{Key: "$lt", Value: q.To}}},
			{Key: "$or", Value: bson.A{
				bson.D{{Key: "end", Value: nil}},
				bson.D{{Key: "end", Value: bson.D{{Key: "$gt", Value: q.From}}}},
			}},
		},
	}}})
	if cats := excludedCategories(q); len(cats) > 0 {
		f = append(f, bson.E{Key: "category", Value: bson.D{{Key: "$nin", Value: cats}}})
	}
	return withAnd(f, and)
}

// placeFilter is §4.1's place query: schedulable categories minus the
// excluded ones, plus the shared conditions. Opening hours are checked in
// Go.
func placeFilter(q planner.CandidateQuery) bson.D {
	f, and := shared(q, "place")
	excluded := excludedCategories(q)
	if len(q.PlaceCategories) > 0 {
		allowed := []string{}
		for _, c := range q.PlaceCategories {
			if !contains(excluded, c) {
				allowed = append(allowed, c)
			}
		}
		f = append(f, bson.E{Key: "category", Value: bson.D{{Key: "$in", Value: allowed}}})
	} else if len(excluded) > 0 {
		f = append(f, bson.E{Key: "category", Value: bson.D{{Key: "$nin", Value: excluded}}})
	}
	return withAnd(f, and)
}

// shared builds the conditions both kinds carry; the second result collects
// the $or clauses that must be combined under $and.
func shared(q planner.CandidateQuery, kind string) (bson.D, bson.A) {
	f := bson.D{}
	var and bson.A
	if q.City != "" {
		f = append(f, bson.E{Key: "city", Value: q.City})
	}
	f = append(f, bson.E{Key: "kind", Value: kind})
	if q.RadiusKm > 0 {
		f = append(f, bson.E{Key: "location", Value: bson.D{{Key: "$geoWithin", Value: bson.D{{Key: "$centerSphere", Value: bson.A{
			bson.A{q.Center.Lng, q.Center.Lat}, q.RadiusKm / earthRadiusKm,
		}}}}}})
	}
	age := planner.AgeRulesFor(q.AgeBracket)
	if tags := union(age.ExcludeTags, q.ExcludeTags); len(tags) > 0 {
		f = append(f, bson.E{Key: "tags", Value: bson.D{{Key: "$nin", Value: tags}}})
	}
	if age.NamePattern != "" {
		f = append(f, bson.E{Key: "name", Value: bson.D{{Key: "$not", Value: bson.Regex{Pattern: age.NamePattern, Options: "i"}}}})
	}
	if ids := objectIDs(q.ExcludeIDs); len(ids) > 0 {
		f = append(f, bson.E{Key: "_id", Value: bson.D{{Key: "$nin", Value: ids}}})
	}
	if len(q.IncludeCategories)+len(q.AnyTags) > 0 {
		var or bson.A
		if len(q.IncludeCategories) > 0 {
			or = append(or, bson.D{{Key: "category", Value: bson.D{{Key: "$in", Value: q.IncludeCategories}}}})
		}
		if len(q.AnyTags) > 0 {
			or = append(or, bson.D{{Key: "tags", Value: bson.D{{Key: "$in", Value: q.AnyTags}}}})
		}
		and = append(and, bson.D{{Key: "$or", Value: or}})
	}
	if p := priceClause(q); p != nil {
		and = append(and, p)
	}
	return f, and
}

// priceClause: a null price is unknown, never free. Free-only keeps known
// free documents and, with no price at all, the categories that are free
// by nature; tiers 1–2 keep the tier, the amount within the tier's bound,
// known-free, and (when allowed) documents that state no amount. The Go
// re-check (planner.MatchesQuery) accepts exactly the same documents.
func priceClause(q planner.CandidateQuery) bson.D {
	if q.FreeOnly {
		return bson.D{{Key: "$or", Value: bson.A{
			bson.D{{Key: "price.isFree", Value: true}},
			bson.D{{Key: "price.min", Value: 0}},
			bson.D{{Key: "price", Value: nil}, {Key: "category", Value: bson.D{{Key: "$in", Value: planner.FreeIfUnknownCategories()}}}},
		}}}
	}
	if q.MaxTier >= 3 {
		return nil
	}
	or := bson.A{
		bson.D{{Key: "price.tier", Value: bson.D{{Key: "$gte", Value: 1}, {Key: "$lte", Value: q.MaxTier}}}},
		bson.D{{Key: "price.min", Value: bson.D{{Key: "$lte", Value: planner.TierBound(q.MaxTier)}}}},
		bson.D{{Key: "price.isFree", Value: true}},
	}
	if q.AllowUnknownPrice {
		or = append(or,
			bson.D{{Key: "price.tier", Value: bson.D{{Key: "$in", Value: bson.A{nil, 0}}}}, {Key: "price.min", Value: nil}},
			bson.D{{Key: "price", Value: nil}},
		)
	}
	return bson.D{{Key: "$or", Value: or}}
}

func excludedCategories(q planner.CandidateQuery) []string {
	return union(planner.AgeRulesFor(q.AgeBracket).ExcludeCategories, q.ExcludeCategories)
}

func withAnd(f bson.D, and bson.A) bson.D {
	if len(and) > 0 {
		f = append(f, bson.E{Key: "$and", Value: and})
	}
	return f
}

// FetchEmbeddings is phase B: the stored vectors of the given documents
// (those that passed the Go-side check), in batches.
func (s *Store) FetchEmbeddings(ctx context.Context, catalog string, ids []string) (map[string][]float64, error) {
	coll, err := s.catalog(catalog)
	if err != nil {
		return nil, err
	}
	oids := objectIDs(ids)
	out := make(map[string][]float64, len(oids))
	for start := 0; start < len(oids); start += fetchBatch {
		chunk := oids[start:min(start+fetchBatch, len(oids))]
		cur, err := coll.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: chunk}}}},
			options.Find().SetProjection(bson.D{{Key: "embedding", Value: 1}}))
		if err != nil {
			return nil, fmt.Errorf("mongosource: fetch embeddings: %w", err)
		}
		var rows []struct {
			ID        bson.ObjectID `bson:"_id"`
			Embedding []float64     `bson:"embedding"`
		}
		if err := cur.All(ctx, &rows); err != nil {
			return nil, fmt.Errorf("mongosource: fetch embeddings: %w", err)
		}
		for _, r := range rows {
			if len(r.Embedding) > 0 {
				out[r.ID.Hex()] = r.Embedding
			}
		}
	}
	return out, nil
}

// FetchTexts returns the embedding texts (the eight-section descriptions
// the reranker reads) of a few documents.
func (s *Store) FetchTexts(ctx context.Context, catalog string, ids []string) (map[string]string, error) {
	coll, err := s.catalog(catalog)
	if err != nil {
		return nil, err
	}
	oids := objectIDs(ids)
	out := make(map[string]string, len(oids))
	if len(oids) == 0 {
		return out, nil
	}
	cur, err := coll.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}},
		options.Find().SetProjection(bson.D{{Key: "embeddingText", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("mongosource: fetch texts: %w", err)
	}
	var rows []struct {
		ID   bson.ObjectID `bson:"_id"`
		Text string        `bson:"embeddingText"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("mongosource: fetch texts: %w", err)
	}
	for _, r := range rows {
		if r.Text != "" {
			out[r.ID.Hex()] = r.Text
		}
	}
	return out, nil
}

// GetActivities fetches documents by id (without their vectors).
func (s *Store) GetActivities(ctx context.Context, catalog string, ids []string) ([]models.Activity, error) {
	coll, err := s.catalog(catalog)
	if err != nil {
		return nil, err
	}
	oids := objectIDs(ids)
	if len(oids) == 0 {
		return nil, nil
	}
	return find(ctx, coll, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: oids}}}}, bson.D{{Key: "_id", Value: 1}}, 0)
}

// --- pools and runs --------------------------------------------------------------

// EnsureIndexes creates the TTL index on expiresAt for plan_pools and
// plan_runs (each document carries its own expiry) and plan_runs by user
// and time. Names and options match store.EnsureIndexes, so either may run
// first and running both is a no-op; an index with the same name but other
// options is an error.
func (s *Store) EnsureIndexes(ctx context.Context) error {
	ttl := func() *options.IndexOptionsBuilder {
		return options.Index().SetName(ExpiresIndex).SetExpireAfterSeconds(0)
	}
	if _, err := s.db.Collection(PoolsCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: ttl()},
		{Keys: bson.D{{Key: "options.stops.id", Value: 1}}, Options: options.Index().SetName(StopIDIndex)},
	}); err != nil {
		return fmt.Errorf("mongosource: %s indexes: %w", PoolsCollection, err)
	}
	if _, err := s.db.Collection(RunsCollection).Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: ttl()},
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName(RunsByUserIndex)},
	}); err != nil {
		return fmt.Errorf("mongosource: %s indexes: %w", RunsCollection, err)
	}
	return nil
}

// SaveRun writes (or replaces) a run document.
func (s *Store) SaveRun(ctx context.Context, run *planner.PlanRun) error {
	_, err := s.db.Collection(RunsCollection).ReplaceOne(ctx, bson.D{{Key: "_id", Value: run.ID}}, run, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("mongosource: save run: %w", err)
	}
	return nil
}

// PatchRun sets fields on a run ("outcome", "ml.jev", …).
func (s *Store) PatchRun(ctx context.Context, id string, patch bson.M) error {
	if len(patch) == 0 {
		return nil
	}
	res, err := s.db.Collection(RunsCollection).UpdateOne(ctx, bson.D{{Key: "_id", Value: id}}, bson.D{{Key: "$set", Value: patch}})
	if err != nil {
		return fmt.Errorf("mongosource: patch run: %w", err)
	}
	if res.MatchedCount == 0 {
		return planner.ErrPoolNotFound
	}
	return nil
}

// SavePool writes (or replaces) a pool document.
func (s *Store) SavePool(ctx context.Context, pool *planner.PlanPool) error {
	doc := *pool
	if doc.Alternatives == nil {
		doc.Alternatives = map[string]planner.Stop{}
	}
	if doc.Scores == nil {
		doc.Scores = map[string]planner.PoolScore{}
	}
	_, err := s.db.Collection(PoolsCollection).ReplaceOne(ctx, bson.D{{Key: "_id", Value: doc.ID}}, &doc, options.Replace().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("mongosource: save pool: %w", err)
	}
	return nil
}

// GetPool returns a live pool; one past its expiry reads as not found even
// before the TTL monitor removes it.
func (s *Store) GetPool(ctx context.Context, runID string) (*planner.PlanPool, error) {
	var pool planner.PlanPool
	err := s.db.Collection(PoolsCollection).FindOne(ctx, s.live(runID)).Decode(&pool)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, planner.ErrPoolNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("mongosource: get pool: %w", err)
	}
	return &pool, nil
}

// FindStop returns the newest live pool holding the stop id, as one of its
// options' stops (indexed) or else a suggested alternative.
func (s *Store) FindStop(ctx context.Context, stopID string) (*planner.PlanPool, *planner.Stop, error) {
	if !safeKey(stopID) {
		return nil, nil, planner.ErrPoolNotFound
	}
	now := s.clock.Now()
	newest := options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetProjection(bson.D{{Key: "queryVector", Value: 0}, {Key: "negVector", Value: 0}})
	for _, where := range []bson.D{
		{{Key: "options.stops.id", Value: stopID}},
		{{Key: "alternatives." + stopID, Value: bson.D{{Key: "$exists", Value: true}}}},
	} {
		filter := append(where, bson.E{Key: "expiresAt", Value: bson.D{{Key: "$gt", Value: now}}})
		var pool planner.PlanPool
		err := s.db.Collection(PoolsCollection).FindOne(ctx, filter, newest).Decode(&pool)
		if errors.Is(err, mongo.ErrNoDocuments) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("mongosource: find stop: %w", err)
		}
		if st, ok := pool.Alternatives[stopID]; ok {
			return &pool, &st, nil
		}
		for _, o := range pool.Options {
			for i := range o.Stops {
				if o.Stops[i].ID == stopID {
					st := o.Stops[i]
					return &pool, &st, nil
				}
			}
		}
	}
	return nil, nil, planner.ErrPoolNotFound
}

// AddAlternatives remembers suggested stops so routes and saves can use
// their ids.
func (s *Store) AddAlternatives(ctx context.Context, runID string, alts []planner.Stop) error {
	set := bson.D{}
	for _, st := range alts {
		if !safeKey(st.ID) {
			return fmt.Errorf("mongosource: bad stop id %q", st.ID)
		}
		set = append(set, bson.E{Key: "alternatives." + st.ID, Value: st})
	}
	return s.updateLivePool(ctx, runID, set)
}

// SetScores merges cached ML scores into the pool.
func (s *Store) SetScores(ctx context.Context, runID string, scores map[string]planner.PoolScore) error {
	set := bson.D{}
	for id, sc := range scores {
		if !safeKey(id) {
			return fmt.Errorf("mongosource: bad activity id %q", id)
		}
		if sc.ML != nil {
			set = append(set, bson.E{Key: "scores." + id + ".ml", Value: *sc.ML})
		}
		if sc.Jev != nil {
			set = append(set, bson.E{Key: "scores." + id + ".jev", Value: *sc.Jev})
		}
	}
	return s.updateLivePool(ctx, runID, set)
}

func (s *Store) updateLivePool(ctx context.Context, runID string, set bson.D) error {
	if len(set) == 0 {
		return nil
	}
	res, err := s.db.Collection(PoolsCollection).UpdateOne(ctx, s.live(runID), bson.D{{Key: "$set", Value: set}})
	if err != nil {
		return fmt.Errorf("mongosource: update pool: %w", err)
	}
	if res.MatchedCount == 0 {
		return planner.ErrPoolNotFound
	}
	return nil
}

func (s *Store) live(runID string) bson.D {
	return bson.D{{Key: "_id", Value: runID}, {Key: "expiresAt", Value: bson.D{{Key: "$gt", Value: s.clock.Now()}}}}
}

// safeKey: ids become field names, so no dots, dollars or empties.
func safeKey(k string) bool {
	return k != "" && !strings.ContainsAny(k, ".$")
}

func objectIDs(hexes []string) []bson.ObjectID {
	out := make([]bson.ObjectID, 0, len(hexes))
	for _, h := range hexes {
		if id, err := bson.ObjectIDFromHex(h); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func union(a, b []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, list := range [][]string{a, b} {
		for _, s := range list {
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
