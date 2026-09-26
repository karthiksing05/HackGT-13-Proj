package handlers

import (
	"Backend/pkg/middleware"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"Backend/pkg/util"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

type CreateCheckoutIntentRequest struct {
	ItemID   string `json:"item_id"`
	Quantity int    `json:"quantity"`
}

type ApproveCheckoutRequest struct {
	PaymentMethodID string `json:"payment_method_id,omitempty"`
}

func CreateCheckoutIntent(w http.ResponseWriter, r *http.Request) {
	uid := middleware.GetUserID(r)
	var req CreateCheckoutIntentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ItemID == "" {
		middleware.WriteError(w, http.StatusBadRequest, "Invalid checkout request body")
		return
	}

	if req.Quantity <= 0 {
		req.Quantity = 1
	}

	// Fetch event details or default pricing
	unitPriceCents := int64(2500) // $25.00
	itemTitle := "Event Ticket"
	if ev, err := store.GlobalStore.GetEventByID(req.ItemID); err == nil {
		if ev.Price != nil && ev.Price.Cents > 0 {
			unitPriceCents = ev.Price.Cents
		}
		itemTitle = ev.Name
	}

	feesCents := int64(350 * req.Quantity) // $3.50 booking fee per ticket
	totalCents := (unitPriceCents * int64(req.Quantity)) + feesCents

	steps := []string{
		"Connecting to ticketing agent",
		"Locating seats in preferred section",
		"Securing ticket reservation hold",
		"Awaiting user approval (Visa checkout)",
	}

	intent := &models.CheckoutIntent{
		ID:             util.GenerateID(),
		UserID:         uid,
		ItemID:         req.ItemID,
		ItemTitle:      itemTitle,
		Quantity:       req.Quantity,
		UnitPriceCents: unitPriceCents,
		FeesCents:      feesCents,
		TotalCents:     totalCents,
		Steps:          steps,
		CurrentStep:    3,
		Status:         "awaiting_approval",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}

	store.GlobalStore.CreateCheckoutIntent(intent)

	// Push over WebSocket
	realtime.GlobalHub.SendToUser(uid, "checkout:status", intent)

	middleware.WriteJSON(w, http.StatusCreated, intent)
}

func GetCheckoutIntent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	intent, err := store.GlobalStore.GetCheckoutIntent(id)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Checkout intent not found")
		return
	}

	// Also push over WebSocket as specified in API_ENDPOINTS.md
	realtime.GlobalHub.SendToUser(intent.UserID, "checkout:status", intent)

	middleware.WriteJSON(w, http.StatusOK, intent)
}

func ApproveCheckoutIntent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	uid := middleware.GetUserID(r)

	intent, err := store.GlobalStore.GetCheckoutIntent(id)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Checkout intent not found")
		return
	}

	if intent.UserID != uid {
		middleware.WriteError(w, http.StatusForbidden, "Forbidden")
		return
	}

	if intent.Status == "completed" {
		middleware.WriteJSON(w, http.StatusOK, intent)
		return
	}

	var req ApproveCheckoutRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	// Validate or select payment method
	card := store.GlobalStore.GetUserCard(uid)
	if card == nil && req.PaymentMethodID == "" {
		// Mock default visa card if user has none added yet
		defaultCard := &models.UserCard{
			Brand: "visa",
			Last4: "4242",
			Token: "tok_visa_default",
		}
		_ = store.GlobalStore.SetUserCard(uid, defaultCard)
		intent.PaymentMethodID = defaultCard.Token
	} else if req.PaymentMethodID != "" {
		intent.PaymentMethodID = req.PaymentMethodID
	} else if card != nil {
		intent.PaymentMethodID = card.Token
	}

	intent.Status = "completed"
	intent.CurrentStep = 4
	intent.Steps = append(intent.Steps, "Payment authorized via Visa", "Tickets confirmed and added to wallet")
	store.GlobalStore.UpdateCheckoutIntent(intent)

	// Broadcast over WebSocket
	realtime.GlobalHub.SendToUser(uid, "checkout:status", intent)

	middleware.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"status":          "completed",
		"checkout_intent": intent,
		"receipt": map[string]interface{}{
			"amount_cents": intent.TotalCents,
			"card_brand":   "Visa",
			"currency":     "USD",
			"paid_at":      time.Now().UTC(),
		},
	})
}

func CancelCheckoutIntent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	uid := middleware.GetUserID(r)

	intent, err := store.GlobalStore.GetCheckoutIntent(id)
	if err != nil {
		middleware.WriteError(w, http.StatusNotFound, "Checkout intent not found")
		return
	}

	if intent.UserID != uid {
		middleware.WriteError(w, http.StatusForbidden, "Forbidden")
		return
	}

	intent.Status = "cancelled"
	store.GlobalStore.UpdateCheckoutIntent(intent)

	realtime.GlobalHub.SendToUser(uid, "checkout:status", intent)

	middleware.WriteJSON(w, http.StatusOK, intent)
}
