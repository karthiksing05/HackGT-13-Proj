package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"net/http"
	"time"
)

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

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"days":  days,
		"count": len(days),
	})
}
