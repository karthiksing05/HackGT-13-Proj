package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

func getBaseHostAndScheme(r *http.Request) (string, string) {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}
	host := r.Host
	if fHost := r.Header.Get("X-Forwarded-Host"); fHost != "" {
		host = fHost
	}
	return scheme, host
}

func buildCalendarLinkResponse(r *http.Request, token string) map[string]interface{} {
	scheme, host := getBaseHostAndScheme(r)
	httpURL := fmt.Sprintf("%s://%s/calendar/feed/%s.ics", scheme, host, token)
	webcalURL := fmt.Sprintf("webcal://%s/calendar/feed/%s.ics", host, token)
	googleCalendarURL := fmt.Sprintf("https://calendar.google.com/calendar/render?cid=%s", url.QueryEscape(webcalURL))

	return map[string]interface{}{
		"token":               token,
		"url":                 httpURL,
		"webcal_url":          webcalURL,
		"google_calendar_url": googleCalendarURL,
		"instructions": map[string]string{
			"apple_calendar":  "Subscribe in Apple Calendar via File > New Calendar Subscription and paste the webcal URL.",
			"google_calendar": "Open Google Calendar on desktop, click '+' next to Other calendars, choose 'From URL', and paste the URL.",
			"outlook":         "In Outlook, choose 'Add calendar' > 'Subscribe from web' and paste the URL.",
		},
	}
}

// GetCalendarLink returns the public .ics subscription link for the authenticated user
func GetCalendarLink(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	token := store.GlobalStore.GetOrCreateCalendarToken(uid)
	middleware.WriteJSON(w, http.StatusOK, buildCalendarLinkResponse(r, token))
}

// RegenerateCalendarLink rotates the calendar token and returns the new public .ics URL
func RegenerateCalendarLink(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	token := store.GlobalStore.RegenerateCalendarToken(uid)
	middleware.WriteJSON(w, http.StatusOK, buildCalendarLinkResponse(r, token))
}

// ServeICSFeed is a public endpoint that returns the RFC 5545 .ics iCalendar file for any calendar app
func ServeICSFeed(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := vars["token"]
	token = strings.TrimSuffix(token, ".ics")

	if token == "" {
		token = r.URL.Query().Get("token")
	}

	userID, err := store.GlobalStore.GetUserIDByCalendarToken(token)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Calendar feed not found or expired. Please regenerate your link in SideQuestz."))
		return
	}

	icsContent, err := store.GlobalStore.GenerateUserICS(userID)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to generate calendar feed")
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline; filename=\"sidequestz.ics\"")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(icsContent))
}

// ExportUserICS allows direct download of the authenticated user's .ics file
func ExportUserICS(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	icsContent, err := store.GlobalStore.GenerateUserICS(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to export calendar")
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"sidequestz-export.ics\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(icsContent))
}

// GetCalendarDays returns merged per-day calendar view for the app UI
func GetCalendarDays(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	q := r.URL.Query()
	fromStr := q.Get("from")
	toStr := q.Get("to")

	now := time.Now()
	fromTime := now
	toTime := now.AddDate(0, 0, 7) // default 7 days

	if parsed, err := time.Parse("2006-01-02", fromStr); err == nil {
		fromTime = parsed
	} else if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
		fromTime = parsed
	}

	if parsed, err := time.Parse("2006-01-02", toStr); err == nil {
		toTime = parsed
	} else if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
		toTime = parsed
	}

	itins, _, _ := store.GlobalStore.ListActiveItineraries(uid, "", 100)

	var days []models.CalendarDay
	cur := fromTime
	for !cur.After(toTime) {
		dateStr := cur.Format("2006-01-02")

		// Merged view: busy blocks (free/busy only)
		busyBlocks := []models.TimeBlock{
			{
				StartTime: time.Date(cur.Year(), cur.Month(), cur.Day(), 9, 0, 0, 0, cur.Location()),
				EndTime:   time.Date(cur.Year(), cur.Month(), cur.Day(), 11, 30, 0, 0, cur.Location()),
				Title:     "Busy",
			},
			{
				StartTime: time.Date(cur.Year(), cur.Month(), cur.Day(), 14, 0, 0, 0, cur.Location()),
				EndTime:   time.Date(cur.Year(), cur.Month(), cur.Day(), 15, 0, 0, 0, cur.Location()),
				Title:     "Busy",
			},
		}

		var sidequests []models.ItinerarySummary
		for _, it := range itins {
			if it.Date == dateStr {
				sidequests = append(sidequests, models.ItinerarySummary{
					ID:           it.ID,
					Title:        it.Title,
					Date:         it.Date,
					StartTime:    it.StartTime,
					BackByTime:   it.BackByTime,
					Status:       it.Status,
					MaxGroupSize: it.MaxGroupSize,
					MemberCount:  len(it.Members),
					StopCount:    len(it.Items),
					CreatedAt:    it.CreatedAt,
				})
			}
		}

		groupEvents := []models.CalendarEventItem{}
		if cur.Weekday() == time.Friday || cur.Weekday() == time.Saturday {
			groupEvents = append(groupEvents, models.CalendarEventItem{
				ID:        "grp_evt_" + dateStr,
				Title:     "BeltLine Sunset Stroll",
				StartTime: time.Date(cur.Year(), cur.Month(), cur.Day(), 18, 30, 0, 0, cur.Location()),
				EndTime:   time.Date(cur.Year(), cur.Month(), cur.Day(), 21, 0, 0, 0, cur.Location()),
				Location:  "Eastside Trail, Atlanta",
			})
		}

		days = append(days, models.CalendarDay{
			Date:        dateStr,
			BusyBlocks:  busyBlocks,
			Sidequests:  sidequests,
			GroupEvents: groupEvents,
		})

		cur = cur.AddDate(0, 0, 1)
	}

	token := store.GlobalStore.GetOrCreateCalendarToken(uid)
	calLink := buildCalendarLinkResponse(r, token)

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"days":          days,
		"count":         len(days),
		"calendar_link": calLink,
	})
}
