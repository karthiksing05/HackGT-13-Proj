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

type UploadPhotoRequest struct {
	PhotoURL string `json:"photo_url"`
	Caption  string `json:"caption,omitempty"`
}

func ListGroupPhotos(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["id"]

	photos := store.GlobalStore.ListGroupPhotos(groupID)
	if photos == nil {
		photos = []*models.GroupPhoto{}
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"photos": photos,
		"count":  len(photos),
	})
}

func UploadGroupPhoto(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["id"]
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var req UploadPhotoRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	photoURL := req.PhotoURL
	if photoURL == "" {
		photoURL = "https://images.unsplash.com/photo-1518998053901-5348d3961a04?w=800&auto=format&fit=crop&q=60"
	}

	photo := &models.GroupPhoto{
		ID:           util.GenerateID(),
		GroupID:      groupID,
		UploaderID:   u.ID.Hex(),
		UploaderName: u.Name,
		PhotoURL:     photoURL,
		Caption:      req.Caption,
		CreatedAt:    time.Now().UTC(),
	}

	store.GlobalStore.AddGroupPhoto(photo)
	middleware.WriteJSON(w, http.StatusCreated, photo)
}

func DeleteGroupPhoto(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	groupID := vars["id"]
	photoID := vars["photoId"]

	if !store.GlobalStore.DeleteGroupPhoto(groupID, photoID) {
		middleware.WriteError(w, http.StatusNotFound, "Photo not found")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Photo removed from album",
	})
}
