package store

import (
	"Backend/pkg/models"
	"context"
	"errors"
	"maps"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// runActiveIndex keeps one running checkout per user and itinerary.
const runActiveIndex = "userId_itineraryId_running_unique"

// maxTranscript caps a run's stored transcript (the newest lines are kept).
const maxTranscript = 200

func init() {
	appIndexes = append(appIndexes,
		indexSpec{coll: CollCheckoutRuns, keys: bson.D{{Key: "userId", Value: 1}, {Key: "itineraryId", Value: 1}},
			name: runActiveIndex, unique: true, partial: bson.D{{Key: "state", Value: models.RunRunning}}},
		indexSpec{coll: CollCheckoutRuns, keys: bson.D{{Key: "state", Value: 1}, {Key: "leaseUntil", Value: 1}}, name: "state_leaseUntil"},
		indexSpec{coll: CollCheckoutIntents, keys: bson.D{{Key: "runId", Value: 1}}, name: "runId", sparse: true},
	)
}

// ErrRunActive is a second run for an itinerary that already has one running.
var ErrRunActive = errors.New("a checkout run is already running for this itinerary")

// CheckoutRuns is the checkout_runs collection: agentic checkouts, their
// budget accounting and the lease the runner holds while it works one.
type CheckoutRuns struct{ s *Store }

func (s *Store) CheckoutRuns() CheckoutRuns { return CheckoutRuns{s} }

func (c CheckoutRuns) coll() *mongo.Collection { return c.s.db.Collection(CollCheckoutRuns) }

// Create stores a new run and its intents. A run already going for the same
// user and itinerary is ErrRunActive (and nothing is written).
func (c CheckoutRuns) Create(ctx context.Context, run *models.CheckoutRun, intents []*models.CheckoutIntent) error {
	now := c.s.BusinessNow(ctx)
	if run.ID == "" {
		run.ID = NewID()
	}
	run.CreatedAt, run.UpdatedAt = now, now
	run.IntentIDs = run.IntentIDs[:0]
	for _, intent := range intents {
		if intent.ID == "" {
			intent.ID = NewID()
		}
		intent.RunID = run.ID
		run.IntentIDs = append(run.IntentIDs, intent.ID)
	}
	if _, err := c.coll().InsertOne(ctx, run); err != nil {
		if duplicateOn(err, runActiveIndex) {
			return ErrRunActive
		}
		return err
	}
	for _, intent := range intents {
		if err := c.s.CheckoutIntents().Insert(ctx, intent); err != nil {
			return err
		}
	}
	return nil
}

// Get loads one of userID's runs; anyone else's is ErrNotFound.
func (c CheckoutRuns) Get(ctx context.Context, userID, id string) (*models.CheckoutRun, error) {
	var run models.CheckoutRun
	if err := decodeOne(c.coll().FindOne(ctx, bson.M{"_id": id, "userId": userID}), &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// ByID loads a run for the runner, whoever owns it.
func (c CheckoutRuns) ByID(ctx context.Context, id string) (*models.CheckoutRun, error) {
	var run models.CheckoutRun
	if err := decodeOne(c.coll().FindOne(ctx, bson.M{"_id": id}), &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// Active is userID's running checkout for an itinerary, if any (ErrNotFound).
func (c CheckoutRuns) Active(ctx context.Context, userID, itineraryID string) (*models.CheckoutRun, error) {
	var run models.CheckoutRun
	err := decodeOne(c.coll().FindOne(ctx, bson.M{"userId": userID, "itineraryId": itineraryID, "state": models.RunRunning}), &run)
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// Claim takes the run's lease until `until` when it is running and nobody
// holds a live lease, and clears reservedCents: whoever held the run before
// has stopped, so nothing is in flight. ok is false when the run is taken or
// finished.
func (c CheckoutRuns) Claim(ctx context.Context, id string, now, until time.Time) (*models.CheckoutRun, bool, error) {
	var run models.CheckoutRun
	err := c.coll().FindOneAndUpdate(ctx,
		bson.M{"_id": id, "state": models.RunRunning, "$or": bson.A{
			bson.M{"leaseUntil": bson.M{"$exists": false}},
			bson.M{"leaseUntil": bson.M{"$lt": now}},
		}},
		bson.M{"$set": bson.M{"leaseUntil": until, "reservedCents": 0, "updatedAt": c.s.Now()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&run)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &run, true, nil
}

// Extend pushes the lease out while the runner is still working.
func (c CheckoutRuns) Extend(ctx context.Context, id string, until time.Time) error {
	_, err := c.coll().UpdateOne(ctx, bson.M{"_id": id, "state": models.RunRunning}, bson.M{"$set": bson.M{"leaseUntil": until}})
	return err
}

// Stale lists running runs whose lease ran out (or was never taken): the
// runner resumes them at startup.
func (c CheckoutRuns) Stale(ctx context.Context, now time.Time) ([]string, error) {
	cursor, err := c.coll().Find(ctx, bson.M{"state": models.RunRunning, "$or": bson.A{
		bson.M{"leaseUntil": bson.M{"$exists": false}},
		bson.M{"leaseUntil": bson.M{"$lt": now}},
	}}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	var docs []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	ids := make([]string, len(docs))
	for i, d := range docs {
		ids[i] = d.ID
	}
	return ids, nil
}

// Reserve holds cents of the budget for a purchase about to be paid. ok is
// false (and nothing changes) when the run is no longer running or the
// purchase would take reserved plus spent above the budget: the budget is
// enforced here, atomically, whatever the agent asks for.
func (c CheckoutRuns) Reserve(ctx context.Context, id string, cents int) (bool, error) {
	if cents <= 0 {
		return false, nil
	}
	res, err := c.coll().UpdateOne(ctx,
		bson.M{"_id": id, "state": models.RunRunning, "$expr": bson.M{"$lte": bson.A{
			bson.M{"$add": bson.A{"$reservedCents", "$spentCents", cents}}, "$budgetCents",
		}}},
		bson.M{"$inc": bson.M{"reservedCents": cents}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// Commit turns a reservation into spend: the purchase went through for
// spent cents (at most what was reserved).
func (c CheckoutRuns) Commit(ctx context.Context, id string, reserved, spent int) error {
	_, err := c.coll().UpdateOne(ctx, bson.M{"_id": id},
		bson.M{"$inc": bson.M{"reservedCents": -reserved, "spentCents": spent}, "$set": bson.M{"updatedAt": c.s.Now()}})
	return err
}

// Release gives a reservation back (the purchase did not go through).
func (c CheckoutRuns) Release(ctx context.Context, id string, reserved int) error {
	_, err := c.coll().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$inc": bson.M{"reservedCents": -reserved}})
	return err
}

// Log appends transcript lines, keeping the newest maxTranscript.
func (c CheckoutRuns) Log(ctx context.Context, id string, events ...models.RunEvent) error {
	if len(events) == 0 {
		return nil
	}
	_, err := c.coll().UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$push": bson.M{"transcript": bson.M{"$each": events, "$slice": -maxTranscript}},
		"$set":  bson.M{"updatedAt": c.s.Now()},
	})
	return err
}

// SetAgent records which agent works the run (muse | fallback).
func (c CheckoutRuns) SetAgent(ctx context.Context, id, agent string) error {
	_, err := c.coll().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"agent": agent}})
	return err
}

// Finish ends a running run: done with its summary. A run cancelled
// meanwhile keeps its state but gets the summary.
func (c CheckoutRuns) Finish(ctx context.Context, id, summary, agentNote string) (*models.CheckoutRun, error) {
	now := c.s.BusinessNow(ctx)
	set := bson.M{"summary": summary, "updatedAt": now, "finishedAt": now, "reservedCents": 0}
	if agentNote != "" {
		set["agentNote"] = agentNote
	}
	_, err := c.coll().UpdateOne(ctx, bson.M{"_id": id, "state": models.RunRunning},
		bson.M{"$set": mergeM(set, bson.M{"state": models.RunDone}), "$unset": bson.M{"leaseUntil": ""}})
	if err != nil {
		return nil, err
	}
	// Cancelled meanwhile: keep the state, still record what happened.
	_, err = c.coll().UpdateOne(ctx, bson.M{"_id": id, "state": models.RunCancelled, "summary": bson.M{"$exists": false}},
		bson.M{"$set": set, "$unset": bson.M{"leaseUntil": ""}})
	if err != nil {
		return nil, err
	}
	return c.ByID(ctx, id)
}

// Cancel stops userID's running run: purchases not started are not made.
// An already finished run is returned as it is.
func (c CheckoutRuns) Cancel(ctx context.Context, userID, id string) (*models.CheckoutRun, error) {
	_, err := c.coll().UpdateOne(ctx, bson.M{"_id": id, "userId": userID, "state": models.RunRunning},
		bson.M{"$set": bson.M{"state": models.RunCancelled, "updatedAt": c.s.BusinessNow(ctx)}})
	if err != nil {
		return nil, err
	}
	return c.Get(ctx, userID, id)
}

// Running reports whether the run is still running (not cancelled or done).
func (c CheckoutRuns) Running(ctx context.Context, id string) (bool, error) {
	n, err := c.coll().CountDocuments(ctx, bson.M{"_id": id, "state": models.RunRunning})
	return n == 1, err
}

func mergeM(a, b bson.M) bson.M {
	out := bson.M{}
	maps.Copy(out, a)
	maps.Copy(out, b)
	return out
}

// ByRun lists a run's intents in the run's order.
func (c CheckoutIntents) ByRun(ctx context.Context, run *models.CheckoutRun) ([]*models.CheckoutIntent, error) {
	cursor, err := c.coll().Find(ctx, bson.M{"runId": run.ID, "userId": run.UserID})
	if err != nil {
		return nil, err
	}
	found := []*models.CheckoutIntent{}
	if err := cursor.All(ctx, &found); err != nil {
		return nil, err
	}
	byID := make(map[string]*models.CheckoutIntent, len(found))
	for _, intent := range found {
		byID[intent.ID] = intent
	}
	out := make([]*models.CheckoutIntent, 0, len(found))
	for _, id := range run.IntentIDs {
		if intent, ok := byID[id]; ok {
			out = append(out, intent)
		}
	}
	return out, nil
}

// RunUpdate applies set (and appends steps) to a run's intent while it is
// in one of the from states, returning the updated intent; ok is false when
// the intent moved on (booked, failed or cancelled meanwhile).
func (c CheckoutIntents) RunUpdate(ctx context.Context, id string, from []string, set bson.M, steps ...models.CheckoutStep) (*models.CheckoutIntent, bool, error) {
	fields := bson.M{"updatedAt": c.s.BusinessNow(ctx)}
	maps.Copy(fields, set)
	update := bson.M{"$set": fields}
	if len(steps) > 0 {
		update["$push"] = bson.M{"steps": bson.M{"$each": steps}}
	}
	var intent models.CheckoutIntent
	err := c.coll().FindOneAndUpdate(ctx,
		bson.M{"_id": id, "runId": bson.M{"$exists": true}, "state": bson.M{"$in": from}}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&intent)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &intent, true, nil
}

// CancelRun cancels a run's intents that have not been bought yet.
func (c CheckoutIntents) CancelRun(ctx context.Context, runID string) error {
	_, err := c.coll().UpdateMany(ctx,
		bson.M{"runId": runID, "state": models.CheckoutProcessing, "orderRef": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"state": models.CheckoutCancelled, "failureCode": models.FailureCancelled, "updatedAt": c.s.BusinessNow(ctx)}})
	return err
}
