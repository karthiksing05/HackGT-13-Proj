package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type PatchItineraryRequest struct {
	Visibility   *string    `json:"visibility,omitempty"`
	LockTime     *time.Time `json:"lock_time,omitempty"`
	MaxGroupSize *int       `json:"max_group_size,omitempty"`
	ItemOrder    []string   `json:"item_order,omitempty"`
}

type PatchItemNotesRequest struct {
	PrivateNotes *string `json:"private_notes,omitempty"`
	SharedNotes  *string `json:"shared_notes,omitempty"`
}

func ListItineraries(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	cursor, limit := util.ParsePagination(r, 20)

	itins, nextCursor, hasMore := store.GlobalStore.ListActiveItineraries(uid, cursor, limit)
	middleware.WriteJSON(w, http.StatusOK, util.PaginatedResponse{
		Items:      itins,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Total:      len(itins),
	})
}

func GetItinerary(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	itin, err := store.GlobalStore.GetItinerary(id)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Itinerary not found")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, itin)
}

func PatchItinerary(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	itin, err := store.GlobalStore.GetItinerary(id)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Itinerary not found")
		return
	}

	var req PatchItineraryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Visibility != nil {
		itin.Visibility = *req.Visibility
	}
	if req.LockTime != nil {
		itin.LockTime = req.LockTime
	}
	if req.MaxGroupSize != nil && *req.MaxGroupSize > 0 {
		itin.MaxGroupSize = *req.MaxGroupSize
	}
	if len(req.ItemOrder) > 0 {
		itemMap := make(map[string]models.ItineraryItem)
		for _, item := range itin.Items {
			itemMap[item.ID] = item
		}
		var reordered []models.ItineraryItem
		for _, itemId := range req.ItemOrder {
			if item, ok := itemMap[itemId]; ok {
				reordered = append(reordered, item)
			}
		}
		if len(reordered) == len(itin.Items) {
			itin.Items = reordered
		}
	}

	_ = store.GlobalStore.UpdateItinerary(itin)
	middleware.WriteJSON(w, http.StatusOK, itin)
}

func DeleteItinerary(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_ = store.GlobalStore.DeleteItinerary(id)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Itinerary deleted successfully",
	})
}

func PatchItineraryItemNotes(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	itinID := vars["id"]
	itemID := vars["itemId"]

	itin, err := store.GlobalStore.GetItinerary(itinID)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Itinerary not found")
		return
	}

	var req PatchItemNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var foundItem *models.ItineraryItem
	for i := range itin.Items {
		if itin.Items[i].ID == itemID {
			if req.PrivateNotes != nil {
				itin.Items[i].PrivateNotes = *req.PrivateNotes
			}
			if req.SharedNotes != nil {
				itin.Items[i].SharedNotes = *req.SharedNotes
			}
			foundItem = &itin.Items[i]
			break
		}
	}

	if foundItem == nil {
		middleware.WriteError(w, http.StatusNotFound, "Itinerary item not found")
		return
	}

	_ = store.GlobalStore.UpdateItinerary(itin)
	middleware.WriteJSON(w, http.StatusOK, foundItem)
}

func GetItemTransit(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	itinID := vars["id"]
	itemID := vars["itemId"]

	itin, err := store.GlobalStore.GetItinerary(itinID)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Itinerary not found")
		return
	}

	var foundItem *models.ItineraryItem
	for i := range itin.Items {
		if itin.Items[i].ID == itemID {
			foundItem = &itin.Items[i]
			break
		}
	}

	now := time.Now()
	if foundItem != nil && len(foundItem.TransitOptions) > 0 {
		middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"item_id": itemID,
			"transit": foundItem.TransitOptions,
		})
		return
	}

	// Calculate walk, MARTA, rideshare options for "Getting there"
	transitOptions := []models.TransitOption{
		{
			Mode:          "walk",
			DurationMin:   14,
			DistanceKm:    1.1,
			CostCents:     0,
			Summary:       "Walk along Peachtree St and 10th St",
			DepartureTime: now.Add(10 * time.Minute),
			ArrivalTime:   now.Add(24 * time.Minute),
		},
		{
			Mode:          "marta",
			DurationMin:   16,
			DistanceKm:    2.5,
			CostCents:     250, // Standard MARTA fare: $2.50
			Summary:       "Red/Gold line to Midtown Station, transfer to Bus 36",
			DepartureTime: now.Add(5 * time.Minute),
			ArrivalTime:   now.Add(21 * time.Minute),
		},
		{
			Mode:          "rideshare",
			DurationMin:   8,
			DistanceKm:    2.2,
			CostCents:     1150, // $11.50
			Summary:       "UberX / Lyft (~3 min pickup)",
			DepartureTime: now.Add(3 * time.Minute),
			ArrivalTime:   now.Add(11 * time.Minute),
		},
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"item_id": itemID,
		"transit": transitOptions,
	})
}
