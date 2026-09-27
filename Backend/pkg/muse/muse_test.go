package muse

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// serveFixture answers every request with a recorded live response and
// hands the decoded request to check.
func serveFixture(t *testing.T, fixture string, check func(map[string]any)) *httptest.Server {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer key-1" {
			t.Errorf("request: %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		check(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
}

func TestCreateParsesAFunctionCall(t *testing.T) {
	srv := serveFixture(t, "tools_response_1.json", func(body map[string]any) {
		if body["model"] != "muse-spark-1.3" || body["instructions"] == "" {
			t.Errorf("body: %v", body)
		}
		tools, _ := body["tools"].([]any)
		if len(tools) != 1 || tools[0].(map[string]any)["type"] != "function" {
			t.Errorf("tools: %v", body["tools"])
		}
	})
	defer srv.Close()
	c := New("key-1", srv.URL, "muse-spark-1.3")
	resp, err := c.Create(context.Background(), Request{
		Instructions: "Buy tickets.",
		Input:        []any{Message{Role: "user", Content: "Get item it_1."}},
		Tools:        []Tool{{Type: "function", Name: "get_offer", Parameters: map[string]any{"type": "object"}, Strict: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := resp.Calls()
	if resp.ID == "" || len(calls) != 1 || calls[0].Name != "get_offer" || calls[0].CallID == "" || !strings.Contains(calls[0].Arguments, "it_1") {
		t.Fatalf("calls: %+v", calls)
	}
	if !strings.Contains(resp.Text(), "Sunset Jazz") {
		t.Fatalf("text: %q", resp.Text())
	}
}

func TestCreateSendsToolOutputsWithThePreviousResponse(t *testing.T) {
	srv := serveFixture(t, "tools_response_2.json", func(body map[string]any) {
		input, _ := body["input"].([]any)
		if body["previous_response_id"] != "resp_1" || len(input) != 1 || input[0].(map[string]any)["type"] != "function_call_output" {
			t.Errorf("body: %v", body)
		}
	})
	defer srv.Close()
	resp, err := New("key-1", srv.URL, "muse-spark-1.3").Create(context.Background(), Request{
		PreviousResponseID: "resp_1",
		Input:              []any{FunctionCallOutput{Type: "function_call_output", CallID: "call_1", Output: `{"total_cents":1346}`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls := resp.Calls(); len(calls) != 1 || calls[0].Name != "finish" {
		t.Fatalf("calls: %+v", calls)
	}
}

func TestCreateBillingErrorIsUnavailableAndHidesTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"code":"billing_not_configured","message":"Add a payment method."}}`))
	}))
	defer srv.Close()
	_, err := New("secret-key-xyz", srv.URL, "m").Create(context.Background(), Request{Input: []any{}})
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret-key-xyz") {
		t.Fatalf("err = %v", err)
	}
}
