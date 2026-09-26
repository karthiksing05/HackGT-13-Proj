package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type PatchMeRequest struct {
	Name         *string              `json:"name,omitempty"`
	Username     *string              `json:"username,omitempty"`
	BirthDate    *string              `json:"birth_date,omitempty"`
	DateOfBirth  *string              `json:"date_of_birth,omitempty"` // Alias for birth_date
	Status       *string              `json:"status,omitempty"`        // "open" | "online" | "not_free"
	LastLocation *models.GeoJSONPoint `json:"last_location,omitempty"` // [lng, lat]
}

type PatchAvatarRequest struct {
	AvatarColor   string `json:"avatar_color"`
	InitialsColor string `json:"initials_color"` // alias
}

type DeviceTokenRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform,omitempty"`
}

func GetMe(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, u)
}

func PatchMe(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var req PatchMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		u.Name = strings.TrimSpace(*req.Name)
	}
	if req.Username != nil && strings.TrimSpace(*req.Username) != "" {
		newUsername := strings.ToLower(strings.TrimSpace(*req.Username))
		newUsername = strings.TrimPrefix(newUsername, "@")
		if util.IsValidUsername(newUsername) {
			u.Username = &newUsername
		}
	}

	dobStr := ""
	if req.BirthDate != nil {
		dobStr = *req.BirthDate
	} else if req.DateOfBirth != nil {
		dobStr = *req.DateOfBirth
	}
	if dobStr != "" {
		var parsedTime time.Time
		var parseErr error
		if parsedTime, parseErr = time.Parse("2006-01-02", dobStr); parseErr != nil {
			parsedTime, parseErr = time.Parse(time.RFC3339, dobStr)
		}
		if parseErr == nil {
			u.BirthDate = &parsedTime
			u.AgeBracket = store.CalculateAgeBracket(u.BirthDate)
		}
	}

	if req.Status != nil {
		st := strings.ToLower(strings.TrimSpace(*req.Status))
		if st == "open" || st == "online" || st == "not_free" || st == "not free" {
			if st == "not free" {
				st = "not_free"
			}
			u.Status = st
			realtime.GlobalHub.Broadcast("friend:status", map[string]string{
				"user_id": u.ID.Hex(),
				"status":  u.Status,
			})
		}
	}

	if req.LastLocation != nil {
		u.LastLocation = req.LastLocation
	}

	if err := store.GlobalStore.UpdateUser(u); err != nil {
		middleware.WriteError(w, http.StatusInternalServerError, "Failed to update profile")
		return
	}

	middleware.WriteJSON(w, http.StatusOK, u)
}

func UploadPhoto(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	contentType := r.Header.Get("Content-Type")
	var photoURL string

	if strings.HasPrefix(contentType, "multipart/form-data") {
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "Failed to parse form")
			return
		}
		file, header, err := r.FormFile("photo")
		if err != nil {
			middleware.WriteError(w, http.StatusBadRequest, "Missing 'photo' file")
			return
		}
		defer file.Close()

		bytes, err := io.ReadAll(file)
		if err != nil {
			middleware.WriteError(w, http.StatusInternalServerError, "Failed to read photo")
			return
		}

		b64 := base64.StdEncoding.EncodeToString(bytes)
		mime := header.Header.Get("Content-Type")
		if mime == "" {
			mime = "image/jpeg"
		}
		photoURL = fmt.Sprintf("data:%s;base64,%s", mime, b64)
	} else {
		var req struct {
			URL  string `json:"url,omitempty"`
			Data string `json:"data,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && (req.URL != "" || req.Data != "") {
			if req.URL != "" {
				photoURL = req.URL
			} else {
				photoURL = req.Data
			}
		} else {
			seed := "user"
			if u.Username != nil {
				seed = *u.Username
			}
			photoURL = fmt.Sprintf("https://api.dicebear.com/7.x/avataaars/svg?seed=%s", seed)
		}
	}

	u.PhotoURL = &photoURL
	_ = store.GlobalStore.UpdateUser(u)

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"photo_url": photoURL,
	})
}

func DeletePhoto(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	u.PhotoURL = nil
	_ = store.GlobalStore.UpdateUser(u)

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Photo removed successfully",
	})
}

func PatchAvatar(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var req PatchAvatarRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	color := req.AvatarColor
	if color == "" {
		color = req.InitialsColor
	}
	color = strings.ToLower(strings.TrimSpace(color))

	validColors := map[string]bool{
		"ink":    true,
		"sage":   true,
		"clay":   true,
		"forest": true,
		"sand":   true,
	}

	if validColors[color] {
		u.AvatarColor = color
	} else {
		u.AvatarColor = "ink"
	}

	_ = store.GlobalStore.UpdateUser(u)

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"avatar_color": u.AvatarColor,
	})
}

func GetPreferences(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, u.Prefs)
}

func PutPreferences(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var prefs models.UserPrefs
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid preferences body")
		return
	}

	u.Prefs = prefs

	// Seed / update taste tags from setup ratings (1..5 scale converted to 0..1 scale)
	if len(prefs.Ratings) > 0 {
		if u.Taste.Tags == nil {
			u.Taste.Tags = make(map[string]float64)
		}
		for cat, rating := range prefs.Ratings {
			score := float64(rating) / 5.0
			u.Taste.Tags[strings.ToLower(cat)] = score
		}
	}

	// Update positive and negative texts for ML Jev reranking
	if prefs.Answers.PerfectAfternoon != "" {
		posText := prefs.Answers.PerfectAfternoon
		if prefs.Answers.PlanAround != "" {
			posText += ". Plan around: " + prefs.Answers.PlanAround
		}
		u.PositiveText = posText
	}
	if prefs.Answers.NeverWant != "" {
		u.NegativeText = prefs.Answers.NeverWant
	}

	// Initialize positive embedding if unset
	if len(u.PositiveEmbedding) == 0 && len(u.Embedding) == 0 {
		var tagKeys []string
		for k, v := range u.Taste.Tags {
			tagKeys = append(tagKeys, fmt.Sprintf("%s:%.2f", k, v))
		}
		seed := fmt.Sprintf("user:%s:%s:%s", u.ID.Hex(), u.Name, strings.Join(tagKeys, ","))
		emb := ml.GenerateDeterministicEmbedding(seed, 1024)
		u.PositiveEmbedding = emb
		u.Embedding = emb
		u.NegativeEmbedding = make([]float64, 1024)
	}

	_ = store.GlobalStore.UpdateUser(u)
	middleware.WriteJSON(w, http.StatusOK, u.Prefs)
}

func GetTasteProfile(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}
	middleware.WriteJSON(w, http.StatusOK, u.Taste)
}

func PostDeviceToken(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	u, err := store.GlobalStore.GetUserByID(uid)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "User not found")
		return
	}

	var req DeviceTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid device token")
		return
	}

	alreadyHas := false
	for _, tok := range u.APNsTokens {
		if tok == req.Token {
			alreadyHas = true
			break
		}
	}
	if !alreadyHas {
		u.APNsTokens = append(u.APNsTokens, req.Token)
		_ = store.GlobalStore.UpdateUser(u)
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":     "registered",
		"token":      req.Token,
		"registered": time.Now().UTC(),
	})
}
