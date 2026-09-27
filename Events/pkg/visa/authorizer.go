package visa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AuthorizeRequest matches the simulated Visa authorization contract.
type AuthorizeRequest struct {
	Token         string `json:"token"`
	Cryptogram    string `json:"cryptogram"`
	MerchantID    string `json:"merchant_id"`
	AmountCents   int    `json:"amount_cents"`
	InstructionID string `json:"instruction_id,omitempty"`
}

// AuthorizeResponse is returned by the Visa network.
type AuthorizeResponse struct {
	Result string `json:"result"` // "approved" | "declined"
	AuthID string `json:"auth_id,omitempty"`
	Reason string `json:"reason,omitempty"` // "over_limit" | "wrong_merchant" | "expired" | "used" | "unknown_token"
}

// Authorizer authorizes payments against the Visa sandbox network.
type Authorizer interface {
	Authorize(ctx context.Context, req AuthorizeRequest) (*AuthorizeResponse, error)
}

// SimulatedNetwork is an in-memory Visa network simulation implementing budget limits and token state.
type SimulatedNetwork struct {
	mu           sync.Mutex
	networkKey   string
	usedTokens   map[string]bool
	instructions map[string]*instructionRecord
}

type instructionRecord struct {
	MaxCents   int
	SpentCents int
}

// NewSimulatedNetwork creates a new simulated Visa network.
func NewSimulatedNetwork(networkKey string) *SimulatedNetwork {
	return &SimulatedNetwork{
		networkKey:   networkKey,
		usedTokens:   make(map[string]bool),
		instructions: make(map[string]*instructionRecord),
	}
}

// SetInstructionBudget registers or updates an instruction budget limit in cents.
func (sn *SimulatedNetwork) SetInstructionBudget(instructionID string, maxCents int) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.instructions[instructionID] = &instructionRecord{
		MaxCents:   maxCents,
		SpentCents: 0,
	}
}

// Authorize performs sandbox authorization checks.
func (sn *SimulatedNetwork) Authorize(ctx context.Context, req AuthorizeRequest) (*AuthorizeResponse, error) {
	sn.mu.Lock()
	defer sn.mu.Unlock()

	// 1. Merchant ID check
	if req.MerchantID != "sidequestz-events" {
		return &AuthorizeResponse{
			Result: "declined",
			Reason: "wrong_merchant",
		}, nil
	}

	// 2. Token format check (must start with sbx_vtok_)
	if !strings.HasPrefix(req.Token, "sbx_vtok_") {
		return &AuthorizeResponse{
			Result: "declined",
			Reason: "unknown_token",
		}, nil
	}

	// 3. Cryptogram format check (must start with sbx_cgm_)
	if !strings.HasPrefix(req.Cryptogram, "sbx_cgm_") {
		return &AuthorizeResponse{
			Result: "declined",
			Reason: "unknown_token",
		}, nil
	}

	// 4. Single-use token check
	if sn.usedTokens[req.Token] {
		return &AuthorizeResponse{
			Result: "declined",
			Reason: "used",
		}, nil
	}

	// 5. Instruction budget check
	if req.InstructionID != "" {
		rec, exists := sn.instructions[req.InstructionID]
		if exists && rec.MaxCents > 0 {
			if rec.SpentCents+req.AmountCents > rec.MaxCents {
				return &AuthorizeResponse{
					Result: "declined",
					Reason: "over_limit",
				}, nil
			}
			rec.SpentCents += req.AmountCents
		}
	}

	// Mark token as used
	sn.usedTokens[req.Token] = true

	// Approved!
	authID := "sbx_auth_" + strings.ReplaceAll(uuid.NewString()[:12], "-", "")
	return &AuthorizeResponse{
		Result: "approved",
		AuthID: authID,
	}, nil
}

// HTTPClientAuthorizer calls an external Visa network over HTTP.
type HTTPClientAuthorizer struct {
	client     *http.Client
	url        string
	networkKey string
}

// NewHTTPClientAuthorizer creates an HTTP client for external Visa sim.
func NewHTTPClientAuthorizer(url, networkKey string) *HTTPClientAuthorizer {
	return &HTTPClientAuthorizer{
		client:     &http.Client{Timeout: 10 * time.Second},
		url:        url,
		networkKey: networkKey,
	}
}

func (h *HTTPClientAuthorizer) Authorize(ctx context.Context, req AuthorizeRequest) (*AuthorizeResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Sandbox-Network-Key", h.networkKey)

	res, err := h.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("visa authorize request failed: %w", err)
	}
	defer res.Body.Close()

	var authRes AuthorizeResponse
	if err := json.NewDecoder(res.Body).Decode(&authRes); err != nil {
		return nil, fmt.Errorf("failed to decode visa authorize response: %w", err)
	}
	return &authRes, nil
}

// ReleaseFunds cancels/reverts spent cents for an instruction (used when order fails after authorization).
func (sn *SimulatedNetwork) ReleaseFunds(instructionID string, amountCents int) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	if rec, exists := sn.instructions[instructionID]; exists {
		rec.SpentCents -= amountCents
		if rec.SpentCents < 0 {
			rec.SpentCents = 0
		}
	}
}

// HTTPHandler returns an http.HandlerFunc for POST /sandbox/visa/authorize.
func (sn *SimulatedNetwork) HTTPHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Validate network key
		if sn.networkKey != "" {
			providedKey := r.Header.Get("X-Sandbox-Network-Key")
			if providedKey != sn.networkKey {
				http.Error(w, "unauthorized network key", http.StatusUnauthorized)
				return
			}
		}

		var req AuthorizeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		res, err := sn.Authorize(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
	}
}
