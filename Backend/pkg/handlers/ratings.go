package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
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

	// Fetch user to update embedding via ML service
	u, _ := store.GlobalStore.GetUserByID(uid)
	if u != nil {
		dim := 1024
		if len(u.PositiveEmbedding) > 0 {
			dim = len(u.PositiveEmbedding)
		} else if len(u.Embedding) > 0 {
			dim = len(u.Embedding)
		}

		// Find activity embedding or generate deterministic vector
		var eventEmb []float64
		if act, err := store.GlobalStore.GetActivityByID(itemID); err == nil && act != nil {
			if len(act.Embedding) == dim {
				eventEmb = act.Embedding
			} else {
				eventEmb = ml.GenerateDeterministicEmbedding("event:"+act.Category+":"+act.Name, dim)
			}
		} else {
			eventEmb = ml.GenerateDeterministicEmbedding("item:"+itemID, dim)
		}

		kind := "positive"
		var currentEmb []float64
		if req.Stars >= 3 {
			kind = "positive"
			if len(u.PositiveEmbedding) == dim {
				currentEmb = u.PositiveEmbedding
			} else if len(u.Embedding) == dim {
				currentEmb = u.Embedding
			} else {
				currentEmb = ml.GenerateDeterministicEmbedding("user:"+u.ID.Hex(), dim)
			}
		} else {
			kind = "negative"
			if len(u.NegativeEmbedding) == dim {
				currentEmb = u.NegativeEmbedding
			} else {
				currentEmb = make([]float64, dim)
			}
		}

		updateReq := &ml.UpdateUserEmbeddingRequest{
			Embedding:      currentEmb,
			Kind:           kind,
			EventEmbedding: eventEmb,
		}

		if updateResp, err := ml.DefaultClient().UpdateUserEmbedding(r.Context(), updateReq); err == nil {
			if kind == "positive" {
				u.PositiveEmbedding = updateResp.Embedding
				u.Embedding = updateResp.Embedding
			} else {
				u.NegativeEmbedding = updateResp.Embedding
			}
			_ = store.GlobalStore.UpdateUser(u)
			log.Info().
				Str("user_id", uid).
				Str("kind", kind).
				Int("stars", req.Stars).
				Msg("User preference embedding successfully updated via ML service")
		} else {
			log.Warn().Err(err).Msg("Failed to update user preference embedding via ML service")
		}
	}

	// Fetch updated taste profile
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
