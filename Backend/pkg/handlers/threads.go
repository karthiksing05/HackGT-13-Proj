package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type SendMessageRequest struct {
	Content     string   `json:"content"`
	Attachments []string `json:"attachments,omitempty"`
}

type StartDMRequest struct {
	RecipientID string `json:"recipient_id"`
}

func ListThreads(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	threads := store.GlobalStore.ListThreads(uid)
	if threads == nil {
		threads = []*models.Thread{}
	}
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"threads": threads,
		"count":   len(threads),
	})
}

func GetThreadMessages(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	threadID := vars["id"]
	before := r.URL.Query().Get("before")
	_, limit := util.ParsePagination(r, 50)

	messages := store.GlobalStore.ListMessages(threadID, before, limit)
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func SendMessage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	threadID := vars["id"]
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Content == "" {
		middleware.WriteError(w, http.StatusBadRequest, "Content is required")
		return
	}

	msg := &models.Message{
		ID:          util.GenerateID(),
		ThreadID:    threadID,
		SenderID:    u.ID.Hex(),
		SenderName:  u.Name,
		SenderPhoto: u.PhotoURL,
		Content:     req.Content,
		Attachments: req.Attachments,
		CreatedAt:   time.Now().UTC(),
	}

	store.GlobalStore.AddMessage(msg)

	// Broadcast to participants via WebSocket
	if thread, err := store.GlobalStore.GetThread(threadID); err == nil {
		realtime.GlobalHub.SendToUsers(thread.ParticipantIDs, "thread:message", msg)
	}

	middleware.WriteJSON(w, http.StatusCreated, msg)
}

func StartDM(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	currentUser, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var req StartDMRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RecipientID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "Missing recipient_id")
		return
	}

	recipient, err := store.GlobalStore.GetUserByID(req.RecipientID)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Recipient user not found")
		return
	}

	// Check if DM thread already exists between these two users
	for _, t := range store.GlobalStore.ListThreads(uid) {
		if t.Type == "dm" {
			for _, pid := range t.ParticipantIDs {
				if pid == req.RecipientID {
					middleware.WriteJSON(w, http.StatusOK, t)
					return
				}
			}
		}
	}

	thread := &models.Thread{
		ID:             util.GenerateID(),
		Type:           "dm",
		Title:          fmt.Sprintf("Chat with %s", recipient.Name),
		ParticipantIDs: []string{uid, recipient.ID.Hex()},
		Participants: []models.UserSummary{
			{
				ID:          currentUser.ID.Hex(),
				Name:        currentUser.Name,
				Username:    currentUser.Username,
				PhotoURL:    currentUser.PhotoURL,
				Status:      currentUser.Status,
				AvatarColor: currentUser.AvatarColor,
			},
			{
				ID:          recipient.ID.Hex(),
				Name:        recipient.Name,
				Username:    recipient.Username,
				PhotoURL:    recipient.PhotoURL,
				Status:      recipient.Status,
				AvatarColor: recipient.AvatarColor,
			},
		},
		UnreadCounts: make(map[string]int),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	store.GlobalStore.CreateThread(thread)
	middleware.WriteJSON(w, http.StatusCreated, thread)
}
