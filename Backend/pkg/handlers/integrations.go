package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type AddPaymentMethodRequest struct {
	Brand string `json:"brand"` // "visa"
	Last4 string `json:"last4"` // "4242"
	Token string `json:"token"`
}

func GetIntegrations(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	calendars := store.GlobalStore.GetUserCalendars(uid)
	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"calendars": calendars,
		"count":     len(calendars),
	})
}

func ConnectIntegration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	provider := strings.ToLower(vars["provider"])
	if provider != "google" && provider != "outlook" {
		middleware.WriteError(w, http.StatusBadRequest, "Unsupported calendar provider. Must be 'google' or 'outlook'")
		return
	}

	uid := middleware.GetUserID(r)
	oauthURL := fmt.Sprintf("https://accounts.%s.com/o/oauth2/v2/auth?client_id=sidequestz_app&redirect_uri=https://sidequestz.app/integrations/%s/callback&state=%s&scope=calendar.events.readonly",
		provider, provider, uid)

	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"provider":  provider,
		"oauth_url": oauthURL,
	})
}

func IntegrationCallback(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	provider := strings.ToLower(vars["provider"])
	uid := r.URL.Query().Get("state")
	if uid == "" {
		uid = middleware.GetUserID(r)
	}

	tokenRef := fmt.Sprintf("tok_%s_%s", provider, util.GenerateID()[:8])
	store.GlobalStore.SetUserCalendar(uid, provider, tokenRef, true)

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"provider":  provider,
		"connected": true,
		"synced_at": time.Now().UTC(),
		"message":   fmt.Sprintf("Successfully connected %s calendar", provider),
	})
}

func DeleteIntegration(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	provider := strings.ToLower(vars["provider"])
	uid := middleware.GetUserID(r)

	store.GlobalStore.SetUserCalendar(uid, provider, "", false)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": fmt.Sprintf("Disconnected %s calendar", provider),
	})
}

func GetPaymentMethods(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	card := store.GlobalStore.GetUserCard(uid)
	var cards []models.UserCard
	if card != nil {
		cards = append(cards, *card)
	}
	middleware.WriteJSON(w, http.StatusOK, cards)
}

func AddPaymentMethod(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	var req AddPaymentMethodRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	brand := req.Brand
	if brand == "" {
		brand = "visa"
	}
	last4 := req.Last4
	if last4 == "" {
		last4 = "4242"
	}
	token := req.Token
	if token == "" {
		token = "tok_visa_" + util.GenerateID()[:8]
	}

	card := &models.UserCard{
		Brand: brand,
		Last4: last4,
		Token: token,
	}

	_ = store.GlobalStore.SetUserCard(uid, card)
	middleware.WriteJSON(w, http.StatusCreated, card)
}

func DeletePaymentMethod(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	_ = store.GlobalStore.SetUserCard(uid, nil)
	middleware.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Payment method removed successfully",
	})
}
