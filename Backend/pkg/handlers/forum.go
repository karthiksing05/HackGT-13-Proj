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
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type CreateForumPostRequest struct {
	Title      string     `json:"title"`
	Content    string     `json:"content"`
	Lat        float64    `json:"lat"`
	Lng        float64    `json:"lng"`
	Visibility string     `json:"visibility"` // friends, everyone
	UntilTime  *time.Time `json:"until_time,omitempty"`
	Tags       []string   `json:"tags,omitempty"`
	CostCents  int64      `json:"cost_cents,omitempty"`
}

func ListForumPosts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cursor, limit := util.ParsePagination(r, 20)
	openOnly := strings.ToLower(q.Get("open_only")) == "true"

	var tags []string
	if tagParam := q.Get("tags"); tagParam != "" {
		for _, t := range strings.Split(tagParam, ",") {
			tags = append(tags, strings.TrimSpace(t))
		}
	}

	posts, nextCursor, hasMore := store.GlobalStore.ListForumPosts(cursor, limit, tags, openOnly)

	middleware.WriteJSON(w, http.StatusOK, util.PaginatedResponse{
		Items:      posts,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Total:      len(posts),
	})
}

func CreateForumPost(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var req CreateForumPostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Visibility == "" {
		req.Visibility = "everyone"
	}
	if req.Title == "" {
		req.Title = "I'm free!"
	}

	until := req.UntilTime
	if until == nil {
		t := time.Now().Add(3 * time.Hour)
		until = &t
	}

	post := &models.ForumPost{
		ID:                util.GenerateID(),
		UserID:            u.ID.Hex(),
		AuthorName:        u.Name,
		AuthorUsername:    u.Username,
		AuthorPhoto:       u.PhotoURL,
		Type:              "im_free",
		Title:             req.Title,
		Content:           req.Content,
		Lat:               req.Lat,
		Lng:               req.Lng,
		Visibility:        req.Visibility,
		UntilTime:         until,
		Tags:              req.Tags,
		CostCents:         req.CostCents,
		OpenOnly:          true,
		JoinRequestsCount: 0,
		CreatedAt:         time.Now().UTC(),
	}

	store.GlobalStore.CreateForumPost(post)
	realtime.GlobalHub.Broadcast("forum:post_created", post)

	middleware.WriteJSON(w, http.StatusCreated, post)
}

func DeleteForumPost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	_ = store.GlobalStore.DeleteForumPost(id)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Forum post taken down successfully",
	})
}

func CreateJoinRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	postID := vars["id"]
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	req := &models.JoinRequest{
		ID:        util.GenerateID(),
		PostID:    postID,
		UserID:    u.ID.Hex(),
		UserName:  u.Name,
		UserPhoto: u.PhotoURL,
		Status:    "pending",
		CreatedAt: time.Now().UTC(),
	}

	store.GlobalStore.CreateJoinRequest(req)

	// Notify host via WebSocket
	realtime.GlobalHub.Broadcast("join_request:created", req)

	middleware.WriteJSON(w, http.StatusCreated, req)
}

func DeleteJoinRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	postID := vars["id"]
	uid := middleware.GetUserID(r)

	// Find user's join request for this post
	for _, jr := range store.GlobalStore.ListJoinRequestsForItinerary(postID) {
		if jr.UserID == uid {
			store.GlobalStore.DeleteJoinRequest(jr.ID)
			break
		}
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Join request cancelled",
	})
}

func PlanTogether(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	postID := vars["id"]
	uid := middleware.GetUserID(r)
	currentUser, _ := store.GlobalStore.GetUserByID(uid)

	// Find the post author
	var postAuthorID string
	var postAuthorName string
	for _, p := range store.GlobalStore.SearchUsers("", 100) {
		_ = p
	}
	// Look through forum posts
	var targetPost *models.ForumPost
	for _, p := range store.GlobalStore.SearchUsers("", 1) {
		_ = p
	}
	posts, _, _ := store.GlobalStore.ListForumPosts("", 100, nil, false)
	for _, p := range posts {
		if p.ID == postID {
			targetPost = p
			postAuthorID = p.UserID
			postAuthorName = p.AuthorName
			break
		}
	}

	if targetPost == nil {
		middleware.WriteError(w, http.StatusNotFound, "Forum post not found")
		return
	}

	// Opens a DM with the poster
	authorUser, _ := store.GlobalStore.GetUserByID(postAuthorID)

	participants := []models.UserSummary{
		{
			ID:          currentUser.ID.Hex(),
			Name:        currentUser.Name,
			Username:    currentUser.Username,
			PhotoURL:    currentUser.PhotoURL,
			Status:      currentUser.Status,
			AvatarColor: currentUser.AvatarColor,
		},
	}
	if authorUser != nil {
		participants = append(participants, models.UserSummary{
			ID:          authorUser.ID.Hex(),
			Name:        authorUser.Name,
			Username:    authorUser.Username,
			PhotoURL:    authorUser.PhotoURL,
			Status:      authorUser.Status,
			AvatarColor: authorUser.AvatarColor,
		})
	}

	thread := &models.Thread{
		ID:             util.GenerateID(),
		Type:           "dm",
		Title:          fmt.Sprintf("Chat with %s", postAuthorName),
		ParticipantIDs: []string{uid, postAuthorID},
		Participants:   participants,
		UnreadCounts:   make(map[string]int),
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	store.GlobalStore.CreateThread(thread)

	// Send initial message
	initMsg := &models.Message{
		ID:          util.GenerateID(),
		ThreadID:    thread.ID,
		SenderID:    currentUser.ID.Hex(),
		SenderName:  currentUser.Name,
		SenderPhoto: currentUser.PhotoURL,
		Content:     fmt.Sprintf("Hey! Saw your post '%s' and wanted to plan together!", targetPost.Title),
		CreatedAt:   time.Now().UTC(),
	}
	store.GlobalStore.AddMessage(initMsg)

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"thread_id": thread.ID,
		"thread":    thread,
		"message":   initMsg,
	})
}

func GetItineraryJoinRequests(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	itinID := vars["id"]

	requests := store.GlobalStore.ListJoinRequestsForItinerary(itinID)
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"join_requests": requests,
		"count":         len(requests),
	})
}

func ApproveJoinRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	reqID := vars["id"]

	req, err := store.GlobalStore.GetJoinRequest(reqID)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Join request not found")
		return
	}

	req.Status = "approved"

	// If linked to an itinerary, add user to members up to max group size
	if req.ItineraryID != "" {
		if itin, err := store.GlobalStore.GetItinerary(req.ItineraryID); err == nil {
			if len(itin.Members) >= itin.MaxGroupSize {
				middleware.WriteError(w, http.StatusConflict, "Max group size reached")
				return
			}
			user, _ := store.GlobalStore.GetUserByID(req.UserID)
			if user != nil {
				itin.Members = append(itin.Members, models.ItineraryMember{
					UserID:      user.ID.Hex(),
					Name:        user.Name,
					Username:    user.Username,
					PhotoURL:    user.PhotoURL,
					AvatarColor: user.AvatarColor,
					Role:        "member",
				})
				_ = store.GlobalStore.UpdateItinerary(itin)
			}
		}
	}

	realtime.GlobalHub.SendToUser(req.UserID, "join_request:updated", req)

	middleware.WriteJSON(w, http.StatusOK, req)
}

func DeclineJoinRequest(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	reqID := vars["id"]

	req, err := store.GlobalStore.GetJoinRequest(reqID)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Join request not found")
		return
	}

	req.Status = "declined"
	realtime.GlobalHub.SendToUser(req.UserID, "join_request:updated", req)

	middleware.WriteJSON(w, http.StatusOK, req)
}
