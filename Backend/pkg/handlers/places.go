package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// SearchActivities handles GET /activities/search?q=...&kind=...&near=...
func SearchActivities(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	near := r.URL.Query().Get("near")
	kind := r.URL.Query().Get("kind")

	activities := store.GlobalStore.SearchActivities(q, near, kind)
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"activities": activities,
		"count":      len(activities),
	})
}

// SearchPlaces handles GET /places/search?q=...&near=...
func SearchPlaces(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	near := r.URL.Query().Get("near")

	places := store.GlobalStore.SearchPlaces(q, near)
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"places": places,
		"count":  len(places),
	})
}

// ReverseGeocode handles GET /places/reverse?lat=...&lng=...
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

// ListActivities handles GET /activities?kind=&near=&radius=&price=&tags=&age_ok=&cursor=&limit=
func ListActivities(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
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
	activities, nextCursor, hasMore := store.GlobalStore.ListActivities(kind, near, radius, tags, maxPrice, userAgeBracket, cursor, limit)

	// If ranking is requested (or client asks for recommendations), rank via ML service
	if q.Get("rank") == "true" || q.Get("recommended") == "true" {
		var currentUser *models.User
		if claims := middleware.GetUserClaims(r); claims != nil {
			currentUser, _ = store.GlobalStore.GetUserByID(claims.UserID)
		}
		activities = ml.DefaultClient().RankActivities(
			r.Context(),
			currentUser,
			activities,
			ml.RankingOptions{Rerank: true},
			q.Get("q"),
			nil,
		)
	}

	middleware.WriteJSON(w, http.StatusOK, util.PaginatedResponse{
		Items:      activities,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Total:      len(activities),
	})
}

// GetActivityDetail handles GET /activities/{id}
func GetActivityDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	activity, err := store.GlobalStore.GetActivityByID(id)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Activity not found")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, activity)
}

// ListEvents handles GET /events?near=&radius=&price=&tags=&age_ok=&cursor=&limit=
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

	// If ranking is requested (or client asks for recommendations), rank via ML service
	if q.Get("rank") == "true" || q.Get("recommended") == "true" {
		var currentUser *models.User
		if claims := middleware.GetUserClaims(r); claims != nil {
			currentUser, _ = store.GlobalStore.GetUserByID(claims.UserID)
		}
		events = ml.DefaultClient().RankActivities(
			r.Context(),
			currentUser,
			events,
			ml.RankingOptions{Rerank: true},
			q.Get("q"),
			nil,
		)
	}

	middleware.WriteJSON(w, http.StatusOK, util.PaginatedResponse{
		Items:      events,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Total:      len(events),
	})
}

// RecommendActivities handles GET /activities/recommendations or GET /events/recommendations
func RecommendActivities(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	near := q.Get("near")
	radius, _ := strconv.ParseFloat(q.Get("radius"), 64)
	maxPrice, _ := strconv.ParseInt(q.Get("price"), 10, 64)
	searchText := q.Get("q")
	kind := q.Get("kind")

	var currentUser *models.User
	userAgeBracket := "21_plus"
	if claims := middleware.GetUserClaims(r); claims != nil {
		if u, err := store.GlobalStore.GetUserByID(claims.UserID); err == nil {
			currentUser = u
			if u.AgeBracket != nil {
				userAgeBracket = *u.AgeBracket
			}
		}
	}

	var tags []string
	if tagParam := q.Get("tags"); tagParam != "" {
		for _, t := range strings.Split(tagParam, ",") {
			tags = append(tags, strings.TrimSpace(t))
		}
	}

	limit := 20
	if lStr := q.Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	activities, _, _ := store.GlobalStore.ListActivities(kind, near, radius, tags, maxPrice, userAgeBracket, "", 50)
	ranked := ml.DefaultClient().RankActivities(
		r.Context(),
		currentUser,
		activities,
		ml.RankingOptions{
			Rerank: true,
			Limit:  &limit,
		},
		searchText,
		nil,
	)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"items": ranked,
		"count": len(ranked),
	})
}

// GetEventDetail handles GET /events/{id}
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
