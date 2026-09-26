package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

func SearchPlaces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	near := r.URL.Query().Get("near")

	places := store.GlobalStore.SearchPlaces(q, near)
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"places": places,
		"count":  len(places),
	})
}

func ReverseGeocode(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")

	lat, err1 := strconv.ParseFloat(latStr, 64)
	lng, err2 := strconv.ParseFloat(lngStr, 64)
	if err1 != nil || err2 != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid or missing lat/lng query parameters")
		return
	}

	place := store.GlobalStore.ReverseGeocode(lat, lng)
	middleware.WriteJSON(w, http.StatusOK, place)
}

func ListEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	near := q.Get("near")
	radius, _ := strconv.ParseFloat(q.Get("radius"), 64)
	maxPrice, _ := strconv.ParseInt(q.Get("price"), 10, 64)
	ageOk := strings.ToLower(q.Get("age_ok")) == "true"

	userAgeBracket := "21_plus"
	if claims := middleware.GetUserClaims(r); claims != nil {
		if u, err := store.GlobalStore.GetUserByID(claims.UserID); err == nil && u.AgeBracket != nil {
			userAgeBracket = *u.AgeBracket
		}
	} else if ageOk {
		// If age_ok requested by unauthenticated client, filter for general all-ages
		userAgeBracket = "13_17"
	}

	var tags []string
	if tagParam := q.Get("tags"); tagParam != "" {
		for _, t := range strings.Split(tagParam, ",") {
			tags = append(tags, strings.TrimSpace(t))
		}
	}

	cursor, limit := util.ParsePagination(r, 20)
	events, nextCursor, hasMore := store.GlobalStore.ListEvents(near, radius, tags, maxPrice, userAgeBracket, cursor, limit)

	middleware.WriteJSON(w, http.StatusOK, util.PaginatedResponse{
		Items:      events,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Total:      len(events),
	})
}

func GetEventDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	event, err := store.GlobalStore.GetEventByID(id)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Event not found")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, event)
}
