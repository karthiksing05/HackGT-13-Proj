package handlers_test

import (
	"Backend/pkg/router"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func setupTestServer() *mux.Router {
	r := mux.NewRouter().StrictSlash(true)
	router.SetupRoutes(r)
	return r
}

func executeRequest(r *mux.Router, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestAuthFlow(t *testing.T) {
	r := setupTestServer()

	// 1. Signup
	signupBody := map[string]string{
		"email":    "tester@example.com",
		"password": "Password123!",
		"name":     "Test Explorer",
	}
	body, _ := json.Marshal(signupBody)
	req, _ := http.NewRequest("POST", "/auth/signup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp := executeRequest(r, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201 on signup, got %d: %s", resp.Code, resp.Body.String())
	}

	var authResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			ID          string `json:"_id"`
			Email       string `json:"email"`
			Name        string `json:"name"`
			AvatarColor string `json:"avatarColor"`
			AgeBracket  string `json:"ageBracket"`
			Status      string `json:"status"`
		} `json:"user"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &authResp)
	if authResp.AccessToken == "" || authResp.RefreshToken == "" {
		t.Fatalf("expected tokens in signup response, got empty")
	}

	token := authResp.AccessToken

	// 2. Login
	loginBody := map[string]string{
		"email":    "tester@example.com",
		"password": "Password123!",
	}
	body, _ = json.Marshal(loginBody)
	req, _ = http.NewRequest("POST", "/auth/login", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on login, got %d", resp.Code)
	}

	// 3. Refresh
	refreshBody := map[string]string{
		"refresh_token": authResp.RefreshToken,
	}
	body, _ = json.Marshal(refreshBody)
	req, _ = http.NewRequest("POST", "/auth/refresh", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on refresh, got %d", resp.Code)
	}

	// 4. Me
	req, _ = http.NewRequest("GET", "/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /me, got %d: %s", resp.Code, resp.Body.String())
	}

	// 5. Patch Me (Updating birthDate to test ageBracket setting)
	patchBody := map[string]interface{}{
		"name":       "Updated Explorer",
		"birth_date": "2006-04-12", // 20 years old -> "18_20"
		"status":     "online",
		"last_location": map[string]interface{}{
			"type":        "Point",
			"coordinates": []float64{-84.3880, 33.7490}, // lng FIRST
		},
	}
	body, _ = json.Marshal(patchBody)
	req, _ = http.NewRequest("PATCH", "/me", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on PATCH /me, got %d", resp.Code)
	}

	var patchedUser struct {
		AgeBracket string `json:"ageBracket"`
		Status     string `json:"status"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &patchedUser)
	if patchedUser.AgeBracket != "18_20" {
		t.Fatalf("expected ageBracket '18_20', got '%s'", patchedUser.AgeBracket)
	}

	// 6. Patch Avatar ("sage")
	avatarBody := map[string]string{
		"avatar_color": "sage",
	}
	body, _ = json.Marshal(avatarBody)
	req, _ = http.NewRequest("PATCH", "/me/avatar", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on PATCH /me/avatar, got %d", resp.Code)
	}

	// 7. Preferences (setup answers steps 3-5)
	spendTier := 2
	company := "small_group"
	pace := "chill"
	prefsBody := map[string]interface{}{
		"ratings": map[string]int{
			"outdoors_parks": 4,
			"live_music":     5,
		},
		"company":    company,
		"pace":       pace,
		"spendTier":  spendTier,
		"flexible":   true,
		"splitStyle": "equal",
		"preferFree": false,
		"answers": map[string]string{
			"perfectAfternoon": "Sunny rooftop with friends",
			"neverWant":        "Overcrowded venues",
			"planAround":       "Great food",
		},
	}
	body, _ = json.Marshal(prefsBody)
	req, _ = http.NewRequest("PUT", "/me/preferences", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on PUT /me/preferences, got %d", resp.Code)
	}

	// 8. Taste profile
	req, _ = http.NewRequest("GET", "/me/taste-profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on GET /me/taste-profile, got %d", resp.Code)
	}

	// 9. Password forgot & verify & reset
	forgotBody := map[string]string{"email": "tester@example.com"}
	body, _ = json.Marshal(forgotBody)
	req, _ = http.NewRequest("POST", "/auth/password/forgot", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on forgot password, got %d", resp.Code)
	}
}

func TestCatalogAndPlanning(t *testing.T) {
	r := setupTestServer()

	// 1. Search places
	req, _ := http.NewRequest("GET", "/places/search?q=Piedmont", nil)
	resp := executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /places/search, got %d", resp.Code)
	}

	// 2. Reverse geocode
	req, _ = http.NewRequest("GET", "/places/reverse?lat=33.7879&lng=-84.3733", nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /places/reverse, got %d", resp.Code)
	}

	// 3. List events (check schema alignment: location with coordinates [lng, lat])
	req, _ = http.NewRequest("GET", "/events?near=Atlanta&radius=10", nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /events, got %d", resp.Code)
	}

	var eventsResp struct {
		Items []struct {
			ID       string `json:"_id"`
			Name     string `json:"name"`
			City     string `json:"city"`
			Kind     string `json:"kind"`
			Location struct {
				Type        string    `json:"type"`
				Coordinates []float64 `json:"coordinates"`
			} `json:"location"`
		} `json:"items"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &eventsResp)
	if len(eventsResp.Items) == 0 {
		t.Fatalf("expected events in catalog")
	}
	if len(eventsResp.Items[0].Location.Coordinates) != 2 {
		t.Fatalf("expected [lng, lat] location coordinates")
	}

	// Create user for planning
	signupBody := map[string]string{
		"email":    "planner@example.com",
		"password": "Password123!",
		"name":     "Planner",
	}
	body, _ := json.Marshal(signupBody)
	req, _ = http.NewRequest("POST", "/auth/signup", bytes.NewBuffer(body))
	resp = executeRequest(r, req)
	var authResp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &authResp)
	token := authResp.AccessToken

	// 4. Generate plans
	planReq := map[string]interface{}{
		"start_location": "Midtown",
		"end_location":   "Inman Park",
		"date":           "2026-09-26",
		"mood_text":      "Vibrant art and good food",
		"budget_cents":   4000,
	}
	body, _ = json.Marshal(planReq)
	req, _ = http.NewRequest("POST", "/plans/generate", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /plans/generate, got %d: %s", resp.Code, resp.Body.String())
	}

	// 5. Create itinerary
	itinReq := map[string]interface{}{
		"title":          "Weekend SideQuest",
		"date":           "2026-09-26",
		"start_time":     "18:00",
		"back_by_time":   "22:00",
		"visibility":     "open",
		"max_group_size": 4,
	}
	body, _ = json.Marshal(itinReq)
	req, _ = http.NewRequest("POST", "/itineraries", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201 on /itineraries, got %d: %s", resp.Code, resp.Body.String())
	}

	var createdItin struct {
		ID    string `json:"id"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &createdItin)

	// 6. Get Item Transit
	if len(createdItin.Items) > 0 {
		transitURL := fmt.Sprintf("/itineraries/%s/items/%s/transit", createdItin.ID, createdItin.Items[0].ID)
		req, _ = http.NewRequest("GET", transitURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp = executeRequest(r, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("expected status 200 on item transit, got %d", resp.Code)
		}
	}
}

func TestGroupSplitsMath(t *testing.T) {
	r := setupTestServer()

	// Create user
	signupBody := map[string]string{
		"email":    "splits@example.com",
		"password": "Password123!",
		"name":     "Splits User",
	}
	body, _ := json.Marshal(signupBody)
	req, _ := http.NewRequest("POST", "/auth/signup", bytes.NewBuffer(body))
	resp := executeRequest(r, req)
	var authResp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &authResp)
	token := authResp.AccessToken

	// Post expense: $10.00 (1000 cents) split between 3 people
	expenseReq := map[string]interface{}{
		"what":         "Dinner at Ponce City Market",
		"amount_cents": 1000,
		"split_between_user_ids": []string{
			"user_1", "user_2", "user_3",
		},
	}
	body, _ = json.Marshal(expenseReq)
	req, _ = http.NewRequest("POST", "/groups/group_123/expenses", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201 on /groups/{id}/expenses, got %d: %s", resp.Code, resp.Body.String())
	}

	var expenseResp struct {
		Shares map[string]int64 `json:"shares"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &expenseResp)

	totalShares := int64(0)
	for _, share := range expenseResp.Shares {
		totalShares += share
	}

	if totalShares != 1000 {
		t.Fatalf("expected total shares to equal 1000 cents, got %d", totalShares)
	}

	// Check balances endpoint
	req, _ = http.NewRequest("GET", "/groups/group_123/balances", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /groups/{id}/balances, got %d", resp.Code)
	}
}

func TestCheckoutFlow(t *testing.T) {
	r := setupTestServer()

	// Create user
	signupBody := map[string]string{
		"email":    "checkout@example.com",
		"password": "Password123!",
		"name":     "Checkout User",
	}
	body, _ := json.Marshal(signupBody)
	req, _ := http.NewRequest("POST", "/auth/signup", bytes.NewBuffer(body))
	resp := executeRequest(r, req)
	var authResp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &authResp)
	token := authResp.AccessToken

	// 1. Create checkout intent for seeded event "6ab72395aa01f0e712679d9b"
	intentReq := map[string]interface{}{
		"item_id":  "6ab72395aa01f0e712679d9b",
		"quantity": 2,
	}
	body, _ = json.Marshal(intentReq)
	req, _ = http.NewRequest("POST", "/checkout/intents", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201 on /checkout/intents, got %d: %s", resp.Code, resp.Body.String())
	}

	var intentResp struct {
		ID         string `json:"id"`
		TotalCents int64  `json:"total_cents"`
		Status     string `json:"status"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &intentResp)

	// 2. Approve checkout intent
	approveURL := fmt.Sprintf("/checkout/intents/%s/approve", intentResp.ID)
	req, _ = http.NewRequest("POST", approveURL, bytes.NewBuffer([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on approve checkout, got %d: %s", resp.Code, resp.Body.String())
	}

	var approveResp struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &approveResp)
	if approveResp.Status != "completed" {
		t.Fatalf("expected status completed, got %s", approveResp.Status)
	}
}

func TestForumAndFriends(t *testing.T) {
	r := setupTestServer()

	// Create user
	signupBody := map[string]string{
		"email":    "social@example.com",
		"password": "Password123!",
		"name":     "Social User",
	}
	body, _ := json.Marshal(signupBody)
	req, _ := http.NewRequest("POST", "/auth/signup", bytes.NewBuffer(body))
	resp := executeRequest(r, req)
	var authResp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &authResp)
	token := authResp.AccessToken

	// 1. List forum posts (public)
	req, _ = http.NewRequest("GET", "/forum/posts", nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /forum/posts, got %d", resp.Code)
	}

	// 2. Create forum post (protected)
	postReq := map[string]interface{}{
		"title":      "Free to hang out tonight",
		"content":    "Down to check out Midtown jazz",
		"visibility": "everyone",
		"lat":        33.7879,
		"lng":        -84.3733,
	}
	body, _ = json.Marshal(postReq)
	req, _ = http.NewRequest("POST", "/forum/posts", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201 on POST /forum/posts, got %d", resp.Code)
	}

	// 3. Search users
	req, _ = http.NewRequest("GET", "/users/search?q=social", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200 on /users/search, got %d", resp.Code)
	}

	// 4. Create invite
	req, _ = http.NewRequest("POST", "/invites", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected status 201 on /invites, got %d", resp.Code)
	}
}
