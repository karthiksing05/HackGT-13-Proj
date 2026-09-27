package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"events/pkg/models"
	"events/pkg/tap"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// crockford alphabet excluding I, L, O, U
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func randomCrockford(length int) string {
	bytes := make([]byte, length)
	_, _ = rand.Read(bytes)
	out := make([]byte, length)
	for i, b := range bytes {
		out[i] = crockford[int(b)%len(crockford)]
	}
	return string(out)
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (d *Deps) reject(w http.ResponseWriter, r *http.Request, status int, code, declineReason string) {
	reason := code
	if declineReason != "" {
		reason = "declined: " + declineReason
	}
	d.recordRejection(r.Context(), r, status, reason)
	writeJSONError(w, status, code, declineReason)
}

func (d *Deps) verifyTAP(w http.ResponseWriter, r *http.Request, tag string) (*tap.ParsedInput, bool) {
	sig, err := tap.Verify(r, d.KeyDirectory, d.Store, tag, d.Cfg.MerchantHost, d.Now().UTC())
	if err != nil {
		d.reject(w, r, http.StatusUnauthorized, "bad_signature", "")
		return nil, false
	}
	return sig, true
}

// ----------------------------------------------------------------- API Handlers

// HandleGetOffer returns a quote for GET /api/events/{slug}/offer?quantity=N.
// Route requires TAP signature tag: agent-browser-auth.
func (d *Deps) HandleGetOffer(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.verifyTAP(w, r, "agent-browser-auth"); !ok {
		return
	}

	ctx := r.Context()
	event, err := d.Store.GetEvent(ctx, mux.Vars(r)["slug"])
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not_found", "")
		return
	}

	qty := 1
	if parsed, err := strconv.Atoi(r.URL.Query().Get("quantity")); err == nil && parsed > 0 && parsed <= 10 {
		qty = parsed
	}

	if d.Store.GetScenario(ctx) == models.ScenarioSoldOut || event.Remaining < qty {
		writeJSONError(w, http.StatusConflict, "sold_out", "")
		return
	}

	now := d.Now().UTC()
	fees := models.CalculateFees(event.UnitCents, qty)
	expiresAt := now.Add(10 * time.Minute)

	quote := &models.Quote{
		QuoteID:        "q_" + randomHex(8),
		Event:          event.SummaryView(),
		QuoteExpiresAt: expiresAt.Format(time.RFC3339),
		Quantity:       qty,
		Available:      event.Remaining,
		Currency:       "usd",
		UnitCents:      event.UnitCents,
		SubtotalCents:  event.UnitCents * qty,
		FeesCents:      fees,
		TotalCents:     (event.UnitCents * qty) + fees,
		MerchantID:     MerchantID,
		Sandbox:        true,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}

	if err := d.Store.SaveQuote(ctx, quote); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal", "")
		return
	}
	writeJSON(w, http.StatusOK, quote)
}

// HandleGetOrder retrieves an existing order for GET /api/orders/{order_id}.
func (d *Deps) HandleGetOrder(w http.ResponseWriter, r *http.Request) {
	if _, ok := d.verifyTAP(w, r, "agent-browser-auth"); !ok {
		return
	}

	order, err := d.Store.GetOrder(r.Context(), mux.Vars(r)["order_id"])
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not_found", "")
		return
	}
	writeJSON(w, http.StatusOK, order)
}

// HandleSetScenario toggles scenarios via POST /_demo/scenario.
func (d *Deps) HandleSetScenario(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("X-Demo-Key")
	if key == "" {
		key = r.URL.Query().Get("key")
	}
	if !d.demoKeyOK(key) {
		writeJSONError(w, http.StatusUnauthorized, "bad_demo_key", "")
		return
	}

	var req models.ScenarioState
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !models.ValidScenario(req.Scenario) {
		http.Error(w, "invalid scenario", http.StatusBadRequest)
		return
	}

	d.Store.SetScenario(r.Context(), req.Scenario)
	writeJSON(w, http.StatusOK, map[string]string{"scenario": req.Scenario})
}

// HandleGetTapKey serves public key directory at GET /sandbox/tap/keys/{keyid}.
func (d *Deps) HandleGetTapKey(w http.ResponseWriter, r *http.Request) {
	keyID := mux.Vars(r)["keyid"]
	pubKey, err := d.KeyDirectory.GetPublicKey(keyID)
	if err != nil {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"keyid":      keyID,
		"alg":        "ed25519",
		"public_key": hex.EncodeToString(pubKey),
	})
}

// HandleHealthz returns service health.
func (d *Deps) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "sidequestz-events",
		"sandbox": true,
	})
}

func (d *Deps) recordRejection(ctx context.Context, r *http.Request, statusCode int, reason string) {
	clientIP := r.RemoteAddr
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		clientIP = fwd
	}
	req := &models.RejectedRequest{
		ID:         uuid.NewString(),
		Method:     r.Method,
		Path:       r.URL.Path,
		StatusCode: statusCode,
		Reason:     reason,
		ClientIP:   clientIP,
		Timestamp:  d.Now().UTC(),
	}
	_ = d.Store.RecordRejectedRequest(ctx, req)
}

func writeJSONError(w http.ResponseWriter, status int, code, declineReason string) {
	writeAPIError(w, status, models.APIError{Code: code, DeclineReason: declineReason})
}

func writeAPIError(w http.ResponseWriter, status int, e models.APIError) {
	if e.Message == "" {
		e.Message = errorMessages[e.Code]
	}
	writeJSON(w, status, e)
}

// errorMessages are the sentences that go with each error code.
var errorMessages = map[string]string{
	"bad_signature":        "This request isn't signed by a trusted agent.",
	"bad_request":          "The request body isn't a valid order.",
	"missing_idempotency":  "Send an Idempotency-Key header with every order.",
	"idempotency_conflict": "This Idempotency-Key was already used for a different order.",
	"not_found":            "There's nothing here.",
	"sold_out":             "Those tickets are sold out.",
	"quote_expired":        "That quote expired. Ask for a new offer.",
	"price_changed":        "The price changed. Review the new total.",
	"quantity_mismatch":    "The quantity doesn't match the quote.",
	"unsupported_payment":  "Pay with a Stripe shared payment token (scheme stripe_spt).",
	"declined":             "The payment was declined.",
	"payments_unavailable": "Payments aren't set up on this merchant yet.",
	"bad_demo_key":         "That demo key isn't right.",
	"internal":             "Something went wrong on our side.",
}
