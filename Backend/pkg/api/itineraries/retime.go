package itineraries

import (
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/travel"
	"sort"
	"time"
)

// defaultLegMinutes is the walk between two places when either has no
// coordinate (the mock server's fixed 15 minutes).
const defaultLegMinutes = 15

// retime lays an itinerary's stops out again after an edit, the port of
// the mock server's retimed(): the stops in order (nil = as they are;
// unknown ids and repeats are skipped) keep their length and run back to
// back from the plan's start, each after a walk leg — travel.Estimate
// between the two places when both have coordinates, else 15 minutes —
// stepping around busy blocks, which stay where they are. There is no leg
// after the last stop. A leg between the same two stops as before keeps its
// id, so travel choices saved on it survive a time change.
func retime(it *models.Itinerary, order []string, newID func() string) []models.ItineraryItem {
	var stops, busy []models.ItineraryItem
	for _, item := range it.Items {
		switch item.Kind {
		case models.ItemTransit:
		case kindBusy:
			busy = append(busy, item)
		default:
			stops = append(stops, item)
		}
	}
	ordered := stops
	if order != nil {
		byID := make(map[string]models.ItineraryItem, len(stops))
		for _, s := range stops {
			byID[s.ID] = s
		}
		ordered = make([]models.ItineraryItem, 0, len(order))
		for _, id := range order {
			if s, ok := byID[id]; ok {
				ordered = append(ordered, s)
				delete(byID, id)
			}
		}
	}

	legIDs := legIDsByRoute(it.Items)
	items := append([]models.ItineraryItem{}, busy...)
	cursor := it.Start
	fromName, fromPlace, fromKey := it.StartPlace.Name, &it.StartPlace, ""
	for _, stop := range ordered {
		length := stop.End.Sub(stop.Start)
		minutes := legMinutes(fromPlace, stop.Place)
		leg := time.Duration(minutes) * time.Minute
		legStart := cursor
		for {
			clash := firstClash(busy, legStart, legStart.Add(leg+length))
			if clash == nil {
				break
			}
			legStart = clash.End
		}
		arrive := legStart.Add(leg)
		to := stop.Title
		if stop.Place != nil && stop.Place.Name != "" {
			to = stop.Place.Name
		}
		id := legIDs[fromKey+"|"+stop.ID]
		if id == "" {
			id = newID()
		}
		items = append(items, models.ItineraryItem{
			ID:          id,
			Kind:        models.ItemTransit,
			Title:       "Walk to " + stop.Title,
			Place:       &models.PlaceDoc{Name: fromName + " → " + to},
			Start:       legStart,
			End:         arrive,
			Description: walkNote,
			LegMode:     string(contract.ModeWalk),
			LegMinutes:  minutes,
		})
		moved := stop
		moved.Start = arrive
		moved.End = arrive.Add(length)
		items = append(items, moved)
		cursor = moved.End
		fromName, fromPlace, fromKey = to, stop.Place, stop.ID
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Start.Before(items[j].Start) })
	return items
}

// legMinutes is the walk between two places: travel.Estimate when both have
// coordinates, else the default.
func legMinutes(from, to *models.PlaceDoc) int {
	if from == nil || to == nil || !from.HasCoordinate() || !to.HasCoordinate() {
		return defaultLegMinutes
	}
	leg := travel.Estimate(travel.Point{Lat: *from.Lat, Lng: *from.Lng}, travel.Point{Lat: *to.Lat, Lng: *to.Lng}, travel.Walk)
	return int(leg.Duration / time.Minute)
}

// firstClash is the first busy block overlapping [start, end).
func firstClash(busy []models.ItineraryItem, start, end time.Time) *models.ItineraryItem {
	for i := range busy {
		if busy[i].Start.Before(end) && busy[i].End.After(start) {
			return &busy[i]
		}
	}
	return nil
}

// legIDsByRoute maps "<from stop id>|<to stop id>" (from "" = the start) to
// the id of the transit item between them.
func legIDsByRoute(items []models.ItineraryItem) map[string]string {
	out := map[string]string{}
	from, pending := "", ""
	for _, item := range items {
		switch item.Kind {
		case models.ItemTransit:
			pending = item.ID
		case kindBusy:
		default:
			if pending != "" {
				out[from+"|"+item.ID] = pending
			}
			from, pending = item.ID, ""
		}
	}
	return out
}
