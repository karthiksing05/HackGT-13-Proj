package itineraries

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"net/http"
	"sort"
	"time"
)

// Calendar range: today and the next 13 days by default, 62 days at most.
const (
	calendarDefaultDays = 14
	calendarMaxDays     = 62
	MsgCalendarRange    = "Pick a range of up to 62 days, ending after it starts."
)

// CalendarDays is GET /calendar/days?from=YYYY-MM-DD&to=YYYY-MM-DD →
// [CalendarDay]: every day in the range (days in X-Time-Zone, empty ones
// included) with the viewer's stops that start on it. Busy blocks are never
// invented: calendar connect is simulated and reads no calendar data.
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
	v, err := newViewer(ctx, h.d, uid, its, false)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	byDay := map[string][]contract.CalendarItem{}
	for _, it := range its {
		for i := range it.Items {
			item := &it.Items[i]
			if item.Kind == models.ItemTransit || item.Start.Before(from) || !item.Start.Before(end) {
				continue
			}
			key := httpx.DayKey(item.Start, tz)
			byDay[key] = append(byDay[key], v.calendarItem(it, item))
		}
	}
	out := make([]contract.CalendarDay, 0, days)
	for i := range days {
		day := time.Date(from.Year(), from.Month(), from.Day()+i, 0, 0, 0, 0, tz)
		key := day.Format("2006-01-02")
		items := byDay[key]
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
