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

func TestCalendarICSFeedSystem(t *testing.T) {
	r := setupTestServer()

	// 1. Signup a test user
	signupBody := map[string]string{
		"email":    "calendar_tester@example.com",
		"password": "Password123!",
		"name":     "Calendar Explorer",
	}
	body, _ := json.Marshal(signupBody)
	req, _ := http.NewRequest("POST", "/auth/signup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp := executeRequest(r, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("signup failed: %d: %s", resp.Code, resp.Body.String())
	}

	var authResp struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &authResp)
	token := authResp.AccessToken

	// 2. Create an itinerary so the calendar has events
	itinReq := map[string]interface{}{
		"title":          "Atlanta BeltLine Adventure",
		"date":           "2026-10-15",
		"start_time":     "14:00",
		"back_by_time":   "18:00",
		"visibility":     "private",
		"max_group_size": 4,
	}
	body, _ = json.Marshal(itinReq)
	req, _ = http.NewRequest("POST", "/itineraries", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("create itinerary failed: %d: %s", resp.Code, resp.Body.String())
	}

	// 3. Get calendar subscription links (authenticated)
	req, _ = http.NewRequest("GET", "/calendar/link", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on /calendar/link, got %d: %s", resp.Code, resp.Body.String())
	}

	var linkResp struct {
		Token             string `json:"token"`
		URL               string `json:"url"`
		WebcalURL         string `json:"webcal_url"`
		GoogleCalendarURL string `json:"google_calendar_url"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &linkResp); err != nil {
		t.Fatalf("failed to decode link response: %v", err)
	}
	if linkResp.Token == "" || linkResp.URL == "" || linkResp.WebcalURL == "" {
		t.Fatalf("expected valid token and URLs, got %+v", linkResp)
	}

	feedToken := linkResp.Token

	// 4. Fetch the public ICS feed WITHOUT any authorization header (as Google / Apple Calendar would)
	feedPath := fmt.Sprintf("/calendar/feed/%s.ics", feedToken)
	req, _ = http.NewRequest("GET", feedPath, nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on public ICS feed, got %d: %s", resp.Code, resp.Body.String())
	}

	contentType := resp.Header().Get("Content-Type")
	if !bytes.Contains([]byte(contentType), []byte("text/calendar")) {
		t.Fatalf("expected Content-Type text/calendar, got %s", contentType)
	}

	icsBody := resp.Body.String()
	if !bytes.Contains([]byte(icsBody), []byte("BEGIN:VCALENDAR")) {
		t.Fatalf("expected BEGIN:VCALENDAR in feed, got:\n%s", icsBody)
	}
	if !bytes.Contains([]byte(icsBody), []byte("Atlanta BeltLine Adventure")) {
		t.Fatalf("expected itinerary title in feed, got:\n%s", icsBody)
	}
	if !bytes.Contains([]byte(icsBody), []byte("END:VCALENDAR")) {
		t.Fatalf("expected END:VCALENDAR in feed, got:\n%s", icsBody)
	}

	// 5. Test direct download / export
	req, _ = http.NewRequest("GET", "/calendar/export.ics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on /calendar/export.ics, got %d", resp.Code)
	}
	if !bytes.Contains(resp.Body.Bytes(), []byte("BEGIN:VCALENDAR")) {
		t.Fatalf("expected BEGIN:VCALENDAR in exported file")
	}

	// 6. Test calendar days endpoint includes calendar_link
	req, _ = http.NewRequest("GET", "/calendar/days", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on /calendar/days, got %d", resp.Code)
	}
	var daysResp struct {
		Days         []interface{}          `json:"days"`
		CalendarLink map[string]interface{} `json:"calendar_link"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &daysResp)
	if daysResp.CalendarLink == nil || daysResp.CalendarLink["token"] != feedToken {
		t.Fatalf("expected calendar_link in /calendar/days, got %+v", daysResp.CalendarLink)
	}

	// 7. Test integrations endpoint includes calendar_feed
	req, _ = http.NewRequest("GET", "/integrations", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on /integrations, got %d", resp.Code)
	}
	var integResp struct {
		CalendarFeed map[string]interface{} `json:"calendar_feed"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &integResp)
	if integResp.CalendarFeed == nil || integResp.CalendarFeed["token"] != feedToken {
		t.Fatalf("expected calendar_feed in /integrations, got %+v", integResp.CalendarFeed)
	}

	// 8. Regenerate calendar link
	req, _ = http.NewRequest("POST", "/calendar/link/regenerate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on regenerate, got %d", resp.Code)
	}

	var regenResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &regenResp)
	if regenResp.Token == "" || regenResp.Token == feedToken {
		t.Fatalf("expected new distinct token, got %s vs old %s", regenResp.Token, feedToken)
	}

	// 9. Old token should now return 404 Not Found
	req, _ = http.NewRequest("GET", fmt.Sprintf("/calendar/feed/%s.ics", feedToken), nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for revoked old token, got %d", resp.Code)
	}

	// 10. New token should return 200 OK
	req, _ = http.NewRequest("GET", fmt.Sprintf("/calendar/feed/%s.ics", regenResp.Token), nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 for new token, got %d", resp.Code)
	}
}

func TestHelloWorldBrowserEndpoint(t *testing.T) {
	r := setupTestServer()

	// 1. Browser GET /hello returns HTML
	req, _ := http.NewRequest("GET", "/hello", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp := executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /hello, got %d", resp.Code)
	}
	contentType := resp.Header().Get("Content-Type")
	if !bytes.Contains([]byte(contentType), []byte("text/html")) {
		t.Fatalf("expected text/html, got %s", contentType)
	}
	bodyStr := resp.Body.String()
	if !bytes.Contains([]byte(bodyStr), []byte("Hello, <span class=\"gradient-text\">World!</span>")) {
		t.Fatalf("expected Hello World headline in HTML")
	}
	if !bytes.Contains([]byte(bodyStr), []byte("Calendar .ICS Feed")) {
		t.Fatalf("expected Calendar ICS feed tester in HTML")
	}

	// Verify root / is not hello world
	req, _ = http.NewRequest("GET", "/", nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on GET /, got %d", resp.Code)
	}

	// 2. API client GET /hello with JSON accept returns JSON
	req, _ = http.NewRequest("GET", "/hello", nil)
	req.Header.Set("Accept", "application/json")
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /hello (JSON), got %d", resp.Code)
	}
	var apiResp struct {
		Message        string `json:"message"`
		Status         string `json:"status"`
		CalendarSystem string `json:"calendar_system"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &apiResp); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if !bytes.Contains([]byte(apiResp.Message), []byte("Hello, World!")) {
		t.Fatalf("expected Hello World message, got %s", apiResp.Message)
	}
	if apiResp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", apiResp.Status)
	}

	// 3. Demo calendar feed endpoint
	req, _ = http.NewRequest("GET", "/test/calendar-demo", nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on /test/calendar-demo, got %d", resp.Code)
	}
	var demoResp struct {
		Status   string `json:"status"`
		Calendar struct {
			URL       string `json:"url"`
			WebcalURL string `json:"webcal_url"`
		} `json:"calendar"`
		RawICS string `json:"raw_ics"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &demoResp); err != nil {
		t.Fatalf("failed to decode demo calendar response: %v", err)
	}
	if demoResp.Status != "ok" || demoResp.Calendar.URL == "" {
		t.Fatalf("expected valid calendar url, got %+v", demoResp)
	}
	if !bytes.Contains([]byte(demoResp.RawICS), []byte("BEGIN:VCALENDAR")) {
		t.Fatalf("expected BEGIN:VCALENDAR in raw_ics, got: %s", demoResp.RawICS)
	}
}

func TestActivitiesFreetimeCatalog(t *testing.T) {
	r := setupTestServer()

	// 1. List activities
	req, _ := http.NewRequest("GET", "/activities", nil)
	resp := executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on /activities, got %d: %s", resp.Code, resp.Body.String())
	}

	var listResp struct {
		Items []struct {
			ID       string `json:"_id"`
			Kind     string `json:"kind"`
			City     string `json:"city"`
			Name     string `json:"name"`
			Category string `json:"category"`
			Location struct {
				Type        string    `json:"type"`
				Coordinates []float64 `json:"coordinates"`
			} `json:"location"`
			WeeklyHours []struct {
				Open  int `json:"open"`
				Close int `json:"close"`
			} `json:"weeklyHours"`
			Trail *struct {
				LengthKm float64 `json:"lengthKm"`
				Loop     bool    `json:"loop"`
			} `json:"trail"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("failed to decode /activities response: %v", err)
	}
	if len(listResp.Items) == 0 {
		t.Fatalf("expected activities in /activities response")
	}

	// 2. Search activities for Homestead
	req, _ = http.NewRequest("GET", "/activities/search?q=Homestead", nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on /activities/search, got %d", resp.Code)
	}
	var searchResp struct {
		Activities []struct {
			ID   string `json:"_id"`
			Name string `json:"name"`
			Kind string `json:"kind"`
		} `json:"activities"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &searchResp); err != nil {
		t.Fatalf("failed to decode /activities/search response: %v", err)
	}
	if searchResp.Count == 0 || len(searchResp.Activities) == 0 {
		t.Fatalf("expected Homestead Trail in search results")
	}

	trailID := searchResp.Activities[0].ID

	// 3. Get single activity detail by ID
	req, _ = http.NewRequest("GET", fmt.Sprintf("/activities/%s", trailID), nil)
	resp = executeRequest(r, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 on /activities/%s, got %d", trailID, resp.Code)
	}
	var detailResp struct {
		ID       string `json:"_id"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Category string `json:"category"`
		Trail    *struct {
			LengthKm float64 `json:"lengthKm"`
			Loop     bool    `json:"loop"`
		} `json:"trail"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("failed to decode activity detail: %v", err)
	}
	if detailResp.Name != "Homestead Trail" {
		t.Fatalf("expected Homestead Trail, got %s", detailResp.Name)
	}
	if detailResp.Trail == nil || !detailResp.Trail.Loop {
		t.Fatalf("expected trail info with loop=true for Homestead Trail")
	}
}
