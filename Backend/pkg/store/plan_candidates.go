package store

import (
	"Backend/pkg/candidates"
	"Backend/pkg/datastore"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"context"
	"sort"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// PlanCandidates is what a plan request can draw on.
type PlanCandidates struct {
	Events  []models.Activity // soonest first
	Places  []models.Activity // best rated first
	Rejects map[string]int    // by reason; Mongo drops most server-side, so these are what Keep caught
}

// FindPlanCandidates returns events and places that pass q, up to the given
// limits. Mongo does the filtering with indexed queries (2dsphere on
// location); every result is re-checked with q.Keep.
func (s *Store) FindPlanCandidates(ctx context.Context, q candidates.Query, eventLimit, placeLimit int) (PlanCandidates, error) {
	out := PlanCandidates{Rejects: map[string]int{}}
	if datastore.IsConnected() {
		if col := datastore.GetCollection("activities"); col != nil {
			find := func(filter bson.M, sortBy bson.D, limit int) ([]models.Activity, error) {
				cur, err := col.Find(ctx, filter, options.Find().SetSort(sortBy).SetLimit(int64(limit)))
				if err != nil {
					return nil, err
				}
				var acts []models.Activity
				if err := cur.All(ctx, &acts); err != nil {
					return nil, err
				}
				kept := acts[:0]
				for i := range acts {
					if ok, reason := q.Keep(&acts[i]); ok {
						kept = append(kept, acts[i])
					} else {
						out.Rejects[reason]++
					}
				}
				return kept, nil
			}
			var err error
			if out.Events, err = find(q.EventFilter(), bson.D{{Key: "start", Value: 1}}, eventLimit); err != nil {
				return out, err
			}
			if out.Places, err = find(q.PlaceFilter(), bson.D{{Key: "rating", Value: -1}}, placeLimit); err != nil {
				return out, err
			}
			return out, nil
		}
	}

	s.mu.RLock()
	all := append([]models.Activity(nil), s.activities...)
	s.mu.RUnlock()
	for i := range all {
		a := &all[i]
		if ok, reason := q.Keep(a); !ok {
			out.Rejects[reason]++
			continue
		}
		if a.Kind == "place" {
			out.Places = append(out.Places, *a)
		} else {
			out.Events = append(out.Events, *a)
		}
	}
	sort.SliceStable(out.Events, func(i, j int) bool { return out.Events[i].Start.Before(*out.Events[j].Start) })
	sort.SliceStable(out.Places, func(i, j int) bool { return rating(out.Places[i]) > rating(out.Places[j]) })
	if len(out.Events) > eventLimit {
		out.Events = out.Events[:eventLimit]
	}
	if len(out.Places) > placeLimit {
		out.Places = out.Places[:placeLimit]
	}
	return out, nil
}

// NearestActivity returns the activity closest to p (used to learn the local
// time zone before any candidates are loaded), or nil.
func (s *Store) NearestActivity(ctx context.Context, p travel.Point) *models.Activity {
	if datastore.IsConnected() {
		if col := datastore.GetCollection("activities"); col != nil {
			var act models.Activity
			err := col.FindOne(ctx, bson.M{"location": bson.M{"$nearSphere": bson.M{
				"$geometry": bson.M{"type": "Point", "coordinates": bson.A{p.Lng, p.Lat}},
			}}}).Decode(&act)
			if err == nil {
				return &act
			}
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var best *models.Activity
	bestKm := -1.0
	for i := range s.activities {
		a := &s.activities[i]
		if len(a.Location.Coordinates) < 2 {
			continue
		}
		d := travel.HaversineKm(p, travel.Point{Lat: a.Location.Coordinates[1], Lng: a.Location.Coordinates[0]})
		if bestKm < 0 || d < bestKm {
			bestKm, best = d, a
		}
	}
	if best == nil {
		return nil
	}
	cp := *best
	return &cp
}

func rating(a models.Activity) float64 {
	if a.Rating == nil {
		return 0
	}
	return *a.Rating
}
