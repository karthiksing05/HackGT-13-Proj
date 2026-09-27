package store

import (
	"Backend/pkg/models"
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// BookedTicket is one of a user's own tickets with the plan and the stop it
// was booked for (GET /me/tickets).
type BookedTicket struct {
	Ticket    models.ItemTicket
	Itinerary *models.Itinerary
	Item      *models.ItineraryItem
}

// Booked lists every ticket userID booked (their item_states with a
// ticket, from a single checkout or a Muse run), each with its itinerary as
// it is now, in no particular order. Membership and status don't matter: a
// plan they left or the host deleted still has the stop (deletes only mark
// the plan). A ticket whose stop is gone (removed from the plan in an edit,
// so its time and place went with it) has nothing left to show and is
// skipped; its ticket page still works.
func (st ItemStates) Booked(ctx context.Context, userID string) ([]BookedTicket, error) {
	cursor, err := st.coll().Find(ctx, bson.M{"userId": userID, "ticket": bson.M{"$ne": nil}})
	if err != nil {
		return nil, err
	}
	var states []*models.ItemState
	if err := cursor.All(ctx, &states); err != nil {
		return nil, err
	}
	out := []BookedTicket{}
	if len(states) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(states))
	seen := map[string]bool{}
	for _, state := range states {
		if !seen[state.ItineraryID] {
			seen[state.ItineraryID] = true
			ids = append(ids, state.ItineraryID)
		}
	}
	cursor, err = st.s.db.Collection(CollItineraries).Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
	if err != nil {
		return nil, err
	}
	var plans []*models.Itinerary
	if err := cursor.All(ctx, &plans); err != nil {
		return nil, err
	}
	byID := make(map[string]*models.Itinerary, len(plans))
	for _, plan := range plans {
		byID[plan.ID] = plan
	}
	for _, state := range states {
		plan := byID[state.ItineraryID]
		if plan == nil || state.Ticket == nil {
			continue
		}
		for i := range plan.Items {
			if plan.Items[i].ID == state.ItemID {
				out = append(out, BookedTicket{Ticket: *state.Ticket, Itinerary: plan, Item: &plan.Items[i]})
				break
			}
		}
	}
	return out, nil
}
