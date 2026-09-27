package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Calendar range: today and the next 13 days by default, 62 days at most.
const (
	calendarDefaultDays = 14
	calendarMaxDays     = 62
	MsgCalendarRange    = "Pick a range of up to 62 days, ending after it starts."
)

// CalendarDays is GET /calendar/days?from=YYYY-MM-DD&to=YYYY-MM-DD →
// [CalendarDay]: every day in the range (days in X-Time-Zone, empty ones
// included) with the viewer's stops that start on it and the busy blocks of
// their calendar (calendar_events) that fall on it, in time order. A busy
// block is listed on every day it overlaps, with its calendar_events id and
// no plan; the copies saved plans keep of them are left out.
func (h *H) CalendarDays(w http.ResponseWriter, r *http.Request) {
	ctx, uid, tz := r.Context(), api.UserID(r), httpx.TZ(r)
	q := r.URL.Query()
	from := httpx.LocalMidnight(h.d.BusinessNow(ctx), tz)
	if s := q.Get("from"); s != "" {
		day, err := httpx.ParseDay(s, tz)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, httpx.GenericBadRequest)
			return
		}
		from = day
	}
	to := from.AddDate(0, 0, calendarDefaultDays-1)
	if s := q.Get("to"); s != "" {
		day, err := httpx.ParseDay(s, tz)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, httpx.GenericBadRequest)
			return
		}
		to = day
	}
	days := daysBetween(from, to) + 1
	if days < 1 || days > calendarMaxDays {
		httpx.Error(w, http.StatusBadRequest, MsgCalendarRange)
		return
	}
	end := time.Date(to.Year(), to.Month(), to.Day()+1, 0, 0, 0, 0, tz)
	its, err := h.d.Store.Itineraries().WithItemsBetween(ctx, uid, from, end)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	busy, err := h.d.Store.CalendarEvents().Overlapping(ctx, uid, from, end)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	v, err := newViewer(ctx, h.d, uid, its, false)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	byDay := map[string][]contract.CalendarItem{}
	for _, it := range its {
		for i := range it.Items {
			item := &it.Items[i]
			if item.Kind == models.ItemTransit || item.Kind == kindBusy || item.Start.Before(from) || !item.Start.Before(end) {
				continue
			}
			key := httpx.DayKey(item.Start, tz)
			byDay[key] = append(byDay[key], v.calendarItem(it, item))
		}
	}
	out := make([]contract.CalendarDay, 0, days)
	for i := range days {
		day := time.Date(from.Year(), from.Month(), from.Day()+i, 0, 0, 0, 0, tz)
		next := time.Date(from.Year(), from.Month(), from.Day()+i+1, 0, 0, 0, 0, tz)
		key := day.Format("2006-01-02")
		items := byDay[key]
		for _, ev := range busy {
			if ev.Start.Before(next) && ev.End.After(day) {
				items = append(items, busyBlock(ev))
			}
		}
		if items == nil {
			items = []contract.CalendarItem{}
		}
		sort.SliceStable(items, func(a, b int) bool {
			if !items[a].Start.Equal(items[b].Start.Time) {
				return items[a].Start.Before(items[b].Start.Time)
			}
			return items[a].ID < items[b].ID
		})
		out = append(out, contract.CalendarDay{ID: key, Date: contract.NewTime(day), Items: items})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// busyBlock is a calendar event on the calendar: its title and times,
// nobody on it, no plan.
func busyBlock(ev models.CalendarEvent) contract.CalendarItem {
	return contract.CalendarItem{
		ID: ev.ID, Kind: contract.KindBusy, Title: ev.Title,
		Start: contract.NewTime(ev.Start), End: contract.NewTime(ev.End),
		People: []contract.PersonRef{}, Interested: []contract.PersonRef{},
	}
}

// busyNote is what a busy block says about itself, by where it came from.
func busyNote(source string) string {
	switch source {
	case "google":
		return "From your Google Calendar. SideQuests plans around it."
	case "outlook":
		return "From your Outlook calendar. SideQuests plans around it."
	}
	return "From your calendar. SideQuests plans around it."
}

// ownCalendarEvent is the viewer's calendar event behind /events/{id}, the
// id the calendar lists a busy block under (not the nested item routes,
// which name a plan). ErrNotFound when it is none of theirs.
func (h *H) ownCalendarEvent(r *http.Request, userID string) (*models.CalendarEvent, error) {
	vars := mux.Vars(r)
	if _, nested := vars["itemId"]; nested {
		return nil, store.ErrNotFound
	}
	return h.d.Store.CalendarEvents().Get(r.Context(), userID, vars["id"])
}

// calendarEvent answers GET /events/{id} for one of the viewer's own busy
// blocks: its title, place and times, what it is, and the viewer's note on
// it. It reports whether id was one (the response is then written).
func (h *H) calendarEvent(w http.ResponseWriter, r *http.Request, userID string) bool {
	ev, err := h.ownCalendarEvent(r, userID)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		api.Fail(w, r, err)
		return true
	}
	states, err := h.d.Store.ItemStates().ForUser(r.Context(), userID, []string{ev.ID})
	if err != nil {
		api.Fail(w, r, err)
		return true
	}
	out := contract.ItineraryItem{
		ID: ev.ID, Kind: contract.KindBusy, Title: ev.Title,
		Start: contract.NewTime(ev.Start), End: contract.NewTime(ev.End),
		Description: view.StrPtr(busyNote(ev.Source)),
		People:      []contract.PersonRef{}, Interested: []contract.PersonRef{},
	}
	if loc := strings.TrimSpace(ev.Location); loc != "" {
		out.Place = &contract.Place{Name: loc}
	}
	if st := states[ev.ID]; st != nil && st.Notes != nil && *st.Notes != "" {
		notes, scope := *st.Notes, contract.NotesPrivate
		out.Notes, out.NotesScope = &notes, &scope
	}
	httpx.JSON(w, http.StatusOK, out)
	return true
}

// calendarEventNotes answers PATCH /events/{id} for one of the viewer's own
// busy blocks: the note is theirs alone (nobody else is on it), whatever
// scope was asked for. It reports whether id was one.
func (h *H) calendarEventNotes(w http.ResponseWriter, r *http.Request, userID string, req contract.ItemNotesPatch) bool {
	ev, err := h.ownCalendarEvent(r, userID)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		api.Fail(w, r, err)
		return true
	}
	var notes *string
	if strings.TrimSpace(req.Notes) != "" {
		text := req.Notes
		notes = &text
	}
	scope := string(contract.NotesPrivate)
	if err := h.d.Store.ItemStates().SetNotes(r.Context(), userID, ev.ID, "", notes, &scope); err != nil {
		api.Fail(w, r, err)
		return true
	}
	httpx.NoContent(w)
	return true
}

// calendarBusyItems are the busy blocks of userID's calendar that overlap
// [from, to), as items of a plan saved over that window: the plan's
// timeline shows what it works around. They are the host's own and only
// the host sees them (view.go).
func calendarBusyItems(events []models.CalendarEvent, newID func() string) []models.ItineraryItem {
	out := make([]models.ItineraryItem, 0, len(events))
	for _, ev := range events {
		item := models.ItineraryItem{
			ID: newID(), Kind: kindBusy, Title: ev.Title, Start: ev.Start, End: ev.End,
			Description: busyNote(ev.Source),
		}
		if loc := strings.TrimSpace(ev.Location); loc != "" {
			item.Place = &models.PlaceDoc{Name: loc}
		}
		out = append(out, item)
	}
	return out
}
