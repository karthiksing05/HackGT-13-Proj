package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type PutRatingRequest struct {
	Stars int      `json:"stars"` // 1-5
	Tags  []string `json:"tags"`
	Note  string   `json:"note"`
}

func GetPastEvents(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	unratedOnly := strings.ToLower(r.URL.Query().Get("unrated")) == "true"

	pastEvents := store.GlobalStore.ListPastEvents(uid, unratedOnly)

	unratedCount := 0
	for _, pe := range store.GlobalStore.ListPastEvents(uid, false) {
		if !pe.Rated {
			unratedCount++
		}
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"past_events":   pastEvents,
		"unrated_count": unratedCount,
	})
}

func PutRating(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	itemID := vars["itemId"]
	uid := middleware.GetUserID(r)

	var req PutRatingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid rating body")
		return
	}

	if req.Stars < 1 || req.Stars > 5 {
		middleware.WriteError(w, http.StatusBadRequest, "Stars must be between 1 and 5")
		return
	}

	rating := &models.Rating{
		ItemID:    itemID,
		UserID:    uid,
		Stars:     req.Stars,
		Tags:      req.Tags,
		Note:      req.Note,
		CreatedAt: time.Now().UTC(),
	}

	if err := store.GlobalStore.SaveRating(rating); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to save rating")
		return
	}

	// Fetch updated taste profile
	u, _ := store.GlobalStore.GetUserByID(uid)
	var taste *models.UserTaste
	if u != nil {
		taste = &u.Taste
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"rating":        rating,
		"taste_profile": taste,
		"message":       "Rating saved and taste profile updated",
	})
}
