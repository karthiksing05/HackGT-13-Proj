package handlers_test

import (
	"Backend/pkg/ml"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// Seeded in-memory event "camoufly (18+ Event)": starts 24 h after the store
// is created, 60 minutes, near Castleberry Hill in Atlanta.
const seededEventID = "6ab72395aa01f0e712679d9b"

type dagStop struct {
	ID         string     `json:"id"`
	PlaceID    string     `json:"place_id"`
	Name       string     `json:"name"`
	ArriveTime *time.Time `json:"arrive_time"`
	DepartTime *time.Time `json:"depart_time"`
}

type dagOption struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	Stops []dagStop `json:"stops"`
	Legs  []struct {
		From string `json:"from_stop_id"`
		To   string `json:"to_stop_id"`
	} `json:"legs"`
}

func signUp(t *testing.T, r *mux.Router, email string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": "Password123!", "name": "Planner"})
	req, _ := http.NewRequest("POST", "/auth/signup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp := executeRequest(r, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("signup: %d %s", resp.Code, resp.Body.String())
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(resp.Body.Bytes(), &out)
	return out.AccessToken
}

func postJSON(r *mux.Router, path, token string, body interface{}) (int, []byte) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", path, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := executeRequest(r, req)
	return resp.Code, resp.Body.Bytes()
}

func TestGeneratePlansWithOptimizer(t *testing.T) {
	orig := ml.DefaultClient()
	ml.SetDefaultClient(ml.NewClient("http://127.0.0.1:1")) // ranking unavailable: unscored candidates
	defer ml.SetDefaultClient(orig)

	r := setupTestServer()
	token := signUp(t, r, fmt.Sprintf("dag-%d@example.com", time.Now().UnixNano()))

	req, _ := http.NewRequest("GET", "/activities/"+seededEventID, nil)
	resp := executeRequest(r, req)
	var act struct {
		Start time.Time `json:"start"`
	}
	if resp.Code != http.StatusOK || json.Unmarshal(resp.Body.Bytes(), &act) != nil || act.Start.IsZero() {
		t.Fatalf("seeded event: %d %s", resp.Code, resp.Body.String())
	}

	ny, _ := time.LoadLocation("America/New_York")
	from := act.Start.Add(-time.Hour).In(ny)
	backBy := act.Start.Add(3 * time.Hour).In(ny)
	home := "33.7443,-84.3754"
	code, body := postJSON(r, "/plans/generate", token, map[string]interface{}{
		"start_location": home,
		"end_location":   home,
		"date":           from.Format("2006-01-02"),
		"start_time":     from.Format("15:04"),
		"back_by_time":   backBy.Format("15:04"),
		"range_km":       5.0,
		"pace":           "balanced",
	})
	if code != http.StatusOK {
		t.Fatalf("generate: %d %s", code, body)
	}
	var gen struct {
		Options []dagOption `json:"options"`
		Planner string      `json:"planner"`
		Cursor  string      `json:"cursor"`
	}
	if err := json.Unmarshal(body, &gen); err != nil {
		t.Fatal(err)
	}
	if gen.Planner != "dag" || len(gen.Options) == 0 {
		t.Fatalf("expected optimizer options, got %s", body)
	}

	found := false
	for _, opt := range gen.Options {
		if len(opt.Legs) != len(opt.Stops)+1 {
			t.Errorf("%s: %d legs for %d stops", opt.Title, len(opt.Legs), len(opt.Stops))
		}
		if opt.Legs[0].From != "start" || opt.Legs[len(opt.Legs)-1].To != "end" {
			t.Errorf("%s: legs should run start -> ... -> end", opt.Title)
		}
		for i, s := range opt.Stops {
			if s.ArriveTime == nil || s.DepartTime == nil {
				t.Fatalf("%s: stop %s has no times", opt.Title, s.Name)
			}
			if s.ArriveTime.Before(from) || s.DepartTime.After(backBy) {
				t.Errorf("%s: stop %s outside the window", opt.Title, s.Name)
			}
			if i > 0 && s.ArriveTime.Before(*opt.Stops[i-1].DepartTime) {
				t.Errorf("%s: stops %d and %d overlap", opt.Title, i-1, i)
			}
			if s.PlaceID == seededEventID {
				found = true
				if !s.ArriveTime.Equal(act.Start) {
					t.Errorf("event scheduled at %v, starts %v", s.ArriveTime, act.Start)
				}
			}
		}
	}
	if !found {
		t.Error("the seeded event in the window should appear in an option")
	}

	// Saving an option keeps the optimizer's times.
	opt := gen.Options[0]
	code, body = postJSON(r, "/itineraries", token, map[string]interface{}{"option_id": opt.ID, "visibility": "just_me"})
	if code != http.StatusCreated {
		t.Fatalf("create itinerary: %d %s", code, body)
	}
	var itin struct {
		Items []struct {
			ArriveTime time.Time `json:"arrive_time"`
		} `json:"items"`
	}
	_ = json.Unmarshal(body, &itin)
	if len(itin.Items) != len(opt.Stops) || !itin.Items[0].ArriveTime.Equal(*opt.Stops[0].ArriveTime) {
		t.Errorf("saved itinerary lost the scheduled times: %s", body)
	}

	// Re-timing the same order reports no lateness.
	var order []string
	for _, s := range opt.Stops {
		order = append(order, s.ID)
	}
	code, body = postJSON(r, "/plans/route", token, map[string]interface{}{"option_id": opt.ID, "stop_order": order})
	var route struct {
		LateFlag  bool `json:"late_flag"`
		StopTimes []struct {
			StopID string `json:"stop_id"`
		} `json:"stop_times"`
		Legs []interface{} `json:"recalculated_legs"`
	}
	if code != http.StatusOK || json.Unmarshal(body, &route) != nil {
		t.Fatalf("route: %d %s", code, body)
	}
	if route.LateFlag || len(route.StopTimes) != len(opt.Stops) || len(route.Legs) != len(opt.Stops)+1 {
		t.Errorf("route of the planned order: %s", body)
	}

	// "More" pages come from the same run.
	if gen.Cursor != "" {
		code, body = postJSON(r, "/plans/generate/more", token, map[string]string{"cursor": gen.Cursor})
		var more struct {
			Options []dagOption `json:"options"`
			Planner string      `json:"planner"`
		}
		if code != http.StatusOK || json.Unmarshal(body, &more) != nil || more.Planner != "dag" || len(more.Options) == 0 {
			t.Errorf("more: %d %s", code, body)
		}
	}
}
