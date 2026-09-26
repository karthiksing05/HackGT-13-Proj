package social

import (
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"context"
)

// itineraryView renders an itinerary for one member, for the
// itinerary.updated events that joins and leaves send. It follows
// backend-contract §4 B's per-viewer rules (the itineraries package owns
// GET /itineraries): is_host, going_count = members; stop kind group when
// more than one member else sidequest, people = the first 3 members and
// extra_going the rest; the member's own notes, transit choice, ticket and
// rating, and shared notes (notes_scope shared) when they have no note.
func itineraryView(it *models.Itinerary, viewerID string, ppl people, st *store.SocialMemberState) contract.Itinerary {
	if st == nil {
		st = &store.SocialMemberState{}
	}
	people := ppl.refs(it.MemberIDs, "", 3)
	extra := max(0, len(it.MemberIDs)-3)
	stopKind := contract.KindSidequest
	if len(it.MemberIDs) > 1 {
		stopKind = contract.KindGroup
	}
	items := make([]contract.ItineraryItem, 0, len(it.Items))
	for _, item := range it.Items {
		out := contract.ItineraryItem{
			ID: item.ID, Kind: stopKind, Title: item.Title,
			Start: contract.NewTime(item.Start), End: contract.NewTime(item.End),
			Description: view.StrPtr(item.Description), WebsiteURL: item.WebsiteURL, Bookable: item.Bookable,
			PriceCents: item.PriceCents, People: []contract.PersonRef{}, Interested: []contract.PersonRef{},
			ActivityID: view.StrPtr(item.ActivityID),
		}
		if item.Place != nil {
			place := placeView(*item.Place)
			out.Place = &place
		}
		if item.Kind == models.ItemTransit {
			out.Kind = contract.KindTransit
		} else {
			out.People, out.ExtraGoing = people, extra
		}
		state := st.Items[item.ID]
		switch {
		case state != nil && state.Notes != nil:
			scope := contract.NotesScope(state.NotesScope)
			if !scope.Valid() {
				scope = contract.NotesPrivate
			}
			out.Notes, out.NotesScope = state.Notes, &scope
		case item.SharedNotes != nil:
			scope := contract.NotesShared
			out.Notes, out.NotesScope = item.SharedNotes, &scope
		}
		if state != nil {
			if mode := contract.TravelMode(state.TransitMode); mode.Valid() {
				out.TransitMode = &mode
			}
			if t := state.Ticket; t != nil {
				out.Ticket = &contract.Ticket{ID: t.ID, Quantity: t.Quantity, TotalCents: t.TotalCents, Confirmation: t.Confirmation, URL: t.URL}
			}
		}
		if rating := st.Ratings[item.ID]; rating != nil {
			tags := rating.Tags
			if tags == nil {
				tags = []string{}
			}
			out.Rating = &contract.Rating{Stars: rating.Stars, Tags: tags, Note: rating.Note}
		}
		items = append(items, out)
	}
	return contract.Itinerary{
		ID: it.ID, Title: it.Title,
		Date: contract.NewTime(it.Date), Start: contract.NewTime(it.Start), BackBy: contract.NewTime(it.BackBy),
		StartPlace: placeView(it.StartPlace), EndPlace: placeView(it.EndPlace),
		Visibility: contract.Visibility(it.Visibility), LockAt: contract.PtrOrNil(it.LockAt), MaxGroupSize: it.MaxGroupSize,
		Items: items, GoingCount: len(it.MemberIDs), IsHost: it.HostID == viewerID,
	}
}

// pushItinerary sends itinerary.updated to each recipient, rendered for them.
func (h *H) pushItinerary(ctx context.Context, it *models.Itinerary, recipients []string) {
	ctx, cancel := eventContext(ctx)
	defer cancel()
	states, err := h.d.Store.Joins().MemberStates(ctx, it.ID)
	if err != nil {
		logEventError(realtime.EventItineraryUpdated, err)
		return
	}
	ppl, err := h.loadPeople(ctx, it.MemberIDs)
	if err != nil {
		logEventError(realtime.EventItineraryUpdated, err)
		return
	}
	for _, id := range recipients {
		realtime.ItineraryUpdated(h.d.Publish(), id, itineraryView(it, id, ppl, states[id]))
	}
}
