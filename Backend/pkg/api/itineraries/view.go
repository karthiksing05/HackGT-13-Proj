package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"context"

	"github.com/rs/zerolog/log"
)

// walkNote is the description of every transit item (MockData.walkNote).
const walkNote = "Route options below. Times update live if you run late."

// peopleShown is how many members a group item lists; the rest are extra_going.
const peopleShown = 3

// kindBusy is a busy block of the host's calendar inside an itinerary: a
// saved plan keeps the ones its window overlaps (create.go), so its
// timeline shows what it works around. Re-timing steps around them; a day
// shift drops them. Only the host sees them (seesItem).
const kindBusy = string(contract.KindBusy)

// seesItem reports whether viewerID sees an item of it: everything but the
// busy blocks of the host's calendar, which only the host sees.
func seesItem(it *models.Itinerary, item *models.ItineraryItem, viewerID string) bool {
	return item.Kind != kindBusy || it.HostID == viewerID
}

// viewer is what rendering needs for one viewer: the members shown on group
// items and, for full renders, the viewer's own item states and ratings and
// the tickets other members booked.
type viewer struct {
	id      string
	base    string
	users   map[string]*models.User
	states  map[string]*models.ItemState
	ratings map[string]*models.Rating
	tickets map[string]*models.ItemTicket // item id → a ticket another member booked
}

// newViewer loads what rendering its for viewerID needs. full adds the
// viewer's notes, travel choices, tickets and ratings (calendar days skip them).
func newViewer(ctx context.Context, d *api.Deps, viewerID string, its []*models.Itinerary, full bool) (*viewer, error) {
	v := &viewer{id: viewerID, base: d.Cfg.PublicBaseURL}
	var memberIDs, itemIDs []string
	seen := map[string]bool{}
	for _, it := range its {
		for i, id := range it.MemberIDs {
			if i == peopleShown {
				break
			}
			if !seen[id] {
				seen[id] = true
				memberIDs = append(memberIDs, id)
			}
		}
		for _, item := range it.Items {
			itemIDs = append(itemIDs, item.ID)
		}
	}
	var err error
	if v.users, err = d.Store.Users().ByIDs(ctx, memberIDs); err != nil {
		return nil, err
	}
	if !full {
		return v, nil
	}
	if v.states, err = d.Store.ItemStates().ForUser(ctx, viewerID, itemIDs); err != nil {
		return nil, err
	}
	if v.ratings, err = d.Store.Ratings().ForUser(ctx, viewerID, itemIDs); err != nil {
		return nil, err
	}
	if v.tickets, err = groupTickets(ctx, d, viewerID, its, v.states); err != nil {
		return nil, err
	}
	return v, nil
}

// groupTickets are the tickets the rest of a group booked: on a plan with
// more than one member, an item the viewer has no ticket for shows the one
// a current member booked (the most recently updated when several did).
// The booking screen tells people exactly that: "Your ticket is saved to
// this sidequest. The group sees it too."
func groupTickets(ctx context.Context, d *api.Deps, viewerID string, its []*models.Itinerary, own map[string]*models.ItemState) (map[string]*models.ItemTicket, error) {
	out := map[string]*models.ItemTicket{}
	planOf := map[string]*models.Itinerary{}
	var itemIDs, members []string
	seen := map[string]bool{}
	for _, it := range its {
		if len(it.MemberIDs) < 2 {
			continue
		}
		for _, item := range it.Items {
			if st := own[item.ID]; st == nil || st.Ticket == nil {
				itemIDs = append(itemIDs, item.ID)
				planOf[item.ID] = it
			}
		}
		for _, id := range it.MemberIDs {
			if id != viewerID && !seen[id] {
				seen[id] = true
				members = append(members, id)
			}
		}
	}
	states, err := d.Store.ItemStates().Tickets(ctx, itemIDs, members)
	if err != nil {
		return nil, err
	}
	for _, st := range states {
		if it := planOf[st.ItemID]; it != nil && out[st.ItemID] == nil && st.UserID != viewerID && it.IsMember(st.UserID) {
			out[st.ItemID] = st.Ticket
		}
	}
	return out, nil
}

// View renders one itinerary for one viewer: is_host, going_count, the
// members on group items and the viewer's own notes, travel choice and
// rating on each item, with their ticket or the group's (groupTickets).
// pkg/api/social can use it for the itinerary.updated a join sends (or
// call PublishUpdated).
func View(ctx context.Context, d *api.Deps, it *models.Itinerary, viewerID string) (contract.Itinerary, error) {
	v, err := newViewer(ctx, d, viewerID, []*models.Itinerary{it}, true)
	if err != nil {
		return contract.Itinerary{}, err
	}
	return v.itinerary(it), nil
}

// Views renders several itineraries for one viewer, in order.
func Views(ctx context.Context, d *api.Deps, its []*models.Itinerary, viewerID string) ([]contract.Itinerary, error) {
	v, err := newViewer(ctx, d, viewerID, its, true)
	if err != nil {
		return nil, err
	}
	out := make([]contract.Itinerary, 0, len(its))
	for _, it := range its {
		out = append(out, v.itinerary(it))
	}
	return out, nil
}

// PublishUpdated sends itinerary.updated to each recipient, rendered for
// them. A render that fails is logged and that recipient skipped: the
// change itself already happened.
func PublishUpdated(ctx context.Context, d *api.Deps, it *models.Itinerary, recipientIDs []string) {
	for _, id := range recipientIDs {
		out, err := View(ctx, d, it, id)
		if err != nil {
			log.Warn().Err(err).Str("itinerary", it.ID).Str("user", id).Msg("itinerary.updated not sent")
			continue
		}
		realtime.ItineraryUpdated(d.Publish(), id, out)
	}
}

// others is ids without one of them.
func others(ids []string, except string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != except {
			out = append(out, id)
		}
	}
	return out
}

func (v *viewer) itinerary(it *models.Itinerary) contract.Itinerary {
	items := make([]contract.ItineraryItem, 0, len(it.Items))
	for i := range it.Items {
		if seesItem(it, &it.Items[i], v.id) {
			items = append(items, v.item(it, &it.Items[i]))
		}
	}
	return contract.Itinerary{
		ID:           it.ID,
		Title:        it.Title,
		Date:         contract.NewTime(it.Date),
		Start:        contract.NewTime(it.Start),
		BackBy:       contract.NewTime(it.BackBy),
		StartPlace:   placeOut(it.StartPlace),
		EndPlace:     placeOut(it.EndPlace),
		Visibility:   contract.Visibility(it.Visibility),
		LockAt:       contract.PtrOrNil(it.LockAt),
		MaxGroupSize: it.MaxGroupSize,
		Items:        items,
		GoingCount:   len(it.MemberIDs),
		IsHost:       it.HostID == v.id,
	}
}

// itemKind is transit for legs, group for the stops of a plan with more
// than one member, sidequest otherwise.
func itemKind(it *models.Itinerary, item *models.ItineraryItem) contract.BlockKind {
	switch item.Kind {
	case models.ItemTransit:
		return contract.KindTransit
	case kindBusy:
		return contract.KindBusy
	}
	if len(it.MemberIDs) > 1 {
		return contract.KindGroup
	}
	return contract.KindSidequest
}

// people is who a group item shows: the first three members, the rest
// counted in extra. Solo stops and transit legs show nobody (as in the mock).
func (v *viewer) people(it *models.Itinerary, kind contract.BlockKind) ([]contract.PersonRef, int) {
	out := []contract.PersonRef{}
	if kind != contract.KindGroup {
		return out, 0
	}
	for i, id := range it.MemberIDs {
		if i == peopleShown {
			break
		}
		if u := v.users[id]; u != nil {
			out = append(out, view.PersonRef(u, v.base))
		}
	}
	return out, max(0, len(it.MemberIDs)-peopleShown)
}

func (v *viewer) item(it *models.Itinerary, item *models.ItineraryItem) contract.ItineraryItem {
	kind := itemKind(it, item)
	people, extra := v.people(it, kind)
	out := contract.ItineraryItem{
		ID:          item.ID,
		Kind:        kind,
		Title:       item.Title,
		Place:       placePtr(item.Place),
		Start:       contract.NewTime(item.Start),
		End:         contract.NewTime(item.End),
		Description: view.StrPtr(item.Description),
		WebsiteURL:  item.WebsiteURL,
		TicketURL:   item.TicketURL,
		Bookable:    item.Bookable,
		PriceCents:  item.PriceCents,
		People:      people,
		Interested:  []contract.PersonRef{},
		ExtraGoing:  extra,
		ActivityID:  view.StrPtr(item.ActivityID),
	}
	state := v.states[item.ID]
	out.Notes, out.NotesScope = notesFor(item, state)
	ticket, mine := v.tickets[item.ID], false
	if state != nil {
		if state.TransitMode != "" {
			mode := contract.TravelMode(state.TransitMode)
			out.TransitMode = &mode
		}
		if state.Ticket != nil {
			ticket, mine = state.Ticket, true
		}
	}
	if out.TransitMode == nil && item.Kind == models.ItemStop {
		out.TransitMode = inboundLegMode(it, item.ID)
	}
	if ticket != nil {
		out.Ticket = &contract.Ticket{ID: ticket.ID, Quantity: ticket.Quantity, TotalCents: ticket.TotalCents, Confirmation: ticket.Confirmation, URL: ticket.URL, Mine: mine}
	}
	out.Rating = ratingOut(v.ratings[item.ID])
	return out
}

// notesFor is the note a viewer sees on an item: their own private note
// wins; otherwise the plan's shared note (notes_scope shared). A viewer who
// shared theirs sees the shared note as it is now, since any member can
// replace or clear it. The viewer's saved scope comes back even without text.
func notesFor(item *models.ItineraryItem, state *models.ItemState) (*string, *contract.NotesScope) {
	var scope *contract.NotesScope
	if state != nil && state.NotesScope != "" {
		s := contract.NotesScope(state.NotesScope)
		scope = &s
	}
	if state != nil && state.Notes != nil && *state.Notes != "" && (scope == nil || *scope != contract.NotesShared) {
		notes := *state.Notes
		return &notes, scope
	}
	if item.SharedNotes != nil && *item.SharedNotes != "" {
		notes, shared := *item.SharedNotes, contract.NotesShared
		return &notes, &shared
	}
	return nil, scope
}

func ratingOut(r *models.Rating) *contract.Rating {
	if r == nil {
		return nil
	}
	tags := r.Tags
	if tags == nil {
		tags = []string{}
	}
	return &contract.Rating{Stars: r.Stars, Tags: tags, Note: r.Note}
}

// calendarItem is a stop as a calendar block (busy blocks come from the
// calendar itself, calendar.go).
func (v *viewer) calendarItem(it *models.Itinerary, item *models.ItineraryItem) contract.CalendarItem {
	kind := itemKind(it, item)
	people, _ := v.people(it, kind)
	id := it.ID
	return contract.CalendarItem{
		ID:          item.ID,
		Kind:        kind,
		Title:       item.Title,
		Start:       contract.NewTime(item.Start),
		End:         contract.NewTime(item.End),
		People:      people,
		Interested:  []contract.PersonRef{},
		ItineraryID: &id,
	}
}

func placeOut(p models.PlaceDoc) contract.Place {
	out := contract.Place{Name: p.Name}
	if p.HasCoordinate() {
		out.Coordinate = &contract.Coordinate{Lat: *p.Lat, Lng: *p.Lng}
	}
	return out
}

func placePtr(p *models.PlaceDoc) *contract.Place {
	if p == nil {
		return nil
	}
	out := placeOut(*p)
	return &out
}

func placeIn(p contract.Place) models.PlaceDoc {
	out := models.PlaceDoc{Name: p.Name}
	if p.Coordinate != nil {
		lat, lng := p.Coordinate.Lat, p.Coordinate.Lng
		out.Lat, out.Lng = &lat, &lng
	}
	return out
}

// inboundLegMode is the mode of the transit leg that leads to a stop, the
// default "Getting there" choice until the member picks one.
func inboundLegMode(it *models.Itinerary, stopID string) *contract.TravelMode {
	for i := range it.Items {
		if it.Items[i].ID != stopID {
			continue
		}
		if i == 0 || it.Items[i-1].Kind != models.ItemTransit {
			return nil
		}
		mode := contract.TravelMode(it.Items[i-1].LegMode)
		if !mode.Valid() {
			return nil
		}
		return &mode
	}
	return nil
}
