package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type SendFriendRequestPayload struct {
	UserID string `json:"user_id"`
}

func ListFriends(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	friends := store.GlobalStore.ListFriends(uid)
	if friends == nil {
		friends = []*models.UserSummary{}
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"friends": friends,
		"count":   len(friends),
	})
}

func SearchUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	_, limit := util.ParsePagination(r, 20)

	users := store.GlobalStore.SearchUsers(q, limit)
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
		"count": len(users),
	})
}

func ListFriendRequests(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	requests := store.GlobalStore.ListFriendRequests(uid)
	if requests == nil {
		requests = []*models.FriendRequest{}
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"requests": requests,
		"count":    len(requests),
	})
}

func SendFriendRequest(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	var req SendFriendRequestPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "Missing user_id")
		return
	}

	if req.UserID == uid {
		middleware.WriteError(w, http.StatusBadRequest, "Cannot send friend request to yourself")
		return
	}

	friendReq, err := store.GlobalStore.CreateFriendRequest(uid, req.UserID)
	if err != nil {
		middleware.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Notify recipient via WebSocket
	realtime.GlobalHub.SendToUser(req.UserID, "friend:request_received", friendReq)

	middleware.WriteJSON(w, http.StatusCreated, friendReq)
}

func AcceptFriendRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	req, err := store.GlobalStore.UpdateFriendRequestStatus(id, "accepted")
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Friend request not found")
		return
	}

	realtime.GlobalHub.SendToUser(req.FromUserID, "friend:request_accepted", req)

	middleware.WriteJSON(w, http.StatusOK, req)
}

func DeclineFriendRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	req, err := store.GlobalStore.UpdateFriendRequestStatus(id, "declined")
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Friend request not found")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, req)
}

func RemoveFriend(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	friendID := vars["id"]
	uid := middleware.GetUserID(r)

	store.GlobalStore.RemoveFriend(uid, friendID)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Friend removed",
	})
}

func CreateInvite(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	invite := store.GlobalStore.CreateInvite(uid)
	middleware.WriteJSON(w, http.StatusCreated, invite)
}
