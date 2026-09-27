package me

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"cmp"
	"net/http"
	"slices"
	"time"
)

// Tickets is GET /me/tickets → [MyTicket]: every ticket the caller booked
// (a single checkout or a Muse run, never one another member of a plan
// booked), with the stop it is for. Upcoming ones (the stop ends at or
// after now on the account's clock, so the demo date for the demo cast)
// come first, soonest first; then past ones, latest first. Tickets for
// plans the caller left or the host deleted still list, without the plan's
// title.
func (h *H) Tickets(w http.ResponseWriter, r *http.Request) {
	ctx, uid := r.Context(), api.UserID(r)
	booked, err := h.d.Store.ItemStates().Booked(ctx, uid)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	out := make([]contract.MyTicket, 0, len(booked))
	for _, b := range booked {
		out = append(out, myTicket(b, uid))
	}
	sortTickets(out, h.d.BusinessNow(ctx))
	httpx.JSON(w, http.StatusOK, out)
}

// myTicket renders one booked ticket for its buyer. The plan's title shows
// while the buyer can still see the plan: it isn't deleted and they're on it.
func myTicket(b store.BookedTicket, viewerID string) contract.MyTicket {
	it, item, t := b.Itinerary, b.Item, b.Ticket
	out := contract.MyTicket{
		Ticket:      contract.Ticket{ID: t.ID, Quantity: t.Quantity, TotalCents: t.TotalCents, Confirmation: t.Confirmation, URL: t.URL, Mine: true},
		ItemID:      item.ID,
		ItineraryID: it.ID,
		Title:       item.Title,
		Start:       contract.NewTime(item.Start),
		End:         contract.NewTime(item.End),
		Place:       ticketPlace(item.Place),
		Kind:        ticketKind(it, item),
	}
	if it.Status != models.ItineraryDeleted && it.IsMember(viewerID) {
		title := it.Title
		out.ItineraryTitle = &title
	}
	return out
}

// ticketKind is the stop's kind as Home shows it (pkg/api/itineraries): a
// group stop on a plan with more than one member, a sidequest otherwise.
func ticketKind(it *models.Itinerary, item *models.ItineraryItem) contract.BlockKind {
	switch {
	case item.Kind == models.ItemTransit:
		return contract.KindTransit
	case item.Kind == string(contract.KindBusy):
		return contract.KindBusy
	case len(it.MemberIDs) > 1:
		return contract.KindGroup
	}
	return contract.KindSidequest
}

// ticketPlace is the stop's place; planned stops always have one, so a
// missing one is an empty name rather than a guess.
func ticketPlace(p *models.PlaceDoc) contract.Place {
	if p == nil {
		return contract.Place{}
	}
	out := contract.Place{Name: p.Name}
	if p.HasCoordinate() {
		out.Coordinate = &contract.Coordinate{Lat: *p.Lat, Lng: *p.Lng}
	}
	return out
}

// sortTickets puts upcoming tickets (end ≥ now) first by start, then past
// ones by start, latest first; ties go by item id so the order is stable.
func sortTickets(tickets []contract.MyTicket, now time.Time) {
	upcoming := func(t contract.MyTicket) bool { return !t.End.Before(now) }
	slices.SortStableFunc(tickets, func(a, b contract.MyTicket) int {
		au, bu := upcoming(a), upcoming(b)
		switch {
		case au && !bu:
			return -1
		case bu && !au:
			return 1
		case au:
			return cmp.Or(a.Start.Compare(b.Start.Time), cmp.Compare(a.ItemID, b.ItemID))
		}
		return cmp.Or(b.Start.Compare(a.Start.Time), cmp.Compare(a.ItemID, b.ItemID))
	})
}
