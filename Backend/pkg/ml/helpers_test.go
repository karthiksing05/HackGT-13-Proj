package ml_test

import (
	"Backend/pkg/ml"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeML is an httptest ML service: a handler per path, request counts and the request bodies.
type fakeML struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	hits   map[string]int
	bodies map[string][][]byte
}

func newFakeML(t *testing.T) *fakeML {
	t.Helper()
	f := &fakeML{t: t, routes: map[string]http.HandlerFunc{}, hits: map[string]int{}, bodies: map[string][][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeML) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("reading request body: %v", err)
	}
	f.mu.Lock()
	f.hits[r.URL.Path]++
	if len(body) > 0 {
		f.bodies[r.URL.Path] = append(f.bodies[r.URL.Path], body)
	}
	handler := f.routes[r.URL.Path]
	f.mu.Unlock()
	if handler == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"detail": "Not Found"})
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	handler(w, r)
}

func (f *fakeML) on(path string, handler http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes[path] = handler
}

func (f *fakeML) reply(path string, status int, payload any) {
	f.on(path, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, status, payload) })
}

func (f *fakeML) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

// body decodes the i-th request body sent to path (-1: the last one).
func (f *fakeML) body(path string, i int) map[string]any {
	f.t.Helper()
	f.mu.Lock()
	bodies := f.bodies[path]
	f.mu.Unlock()
	if i < 0 {
		i += len(bodies)
	}
	if i < 0 || i >= len(bodies) {
		f.t.Fatalf("no request body %d for %s (have %d)", i, path, len(bodies))
	}
	var out map[string]any
	if err := json.Unmarshal(bodies[i], &out); err != nil {
		f.t.Fatalf("request body for %s is not JSON: %v", path, err)
	}
	return out
}

func (f *fakeML) client(mutate ...func(*ml.Options)) *ml.Client {
	o := ml.DefaultOptions()
	o.BaseURL = f.srv.URL
	for _, m := range mutate {
		m(&o)
	}
	return ml.NewClientWithOptions(o)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		panic(err)
	}
}

// slow answers after d, or not at all when the client gives up first.
func slow(d time.Duration, payload any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(d):
			writeJSON(w, http.StatusOK, payload)
		case <-r.Context().Done():
		}
	}
}

// basis is the unit vector along axis i.
func basis(dim, i int) []float64 {
	v := make([]float64, dim)
	v[i%dim] = 1
	return v
}

func filled(dim int, x float64) []float64 {
	v := make([]float64, dim)
	for i := range v {
		v[i] = x
	}
	return v
}

func healthPayload(jev bool) map[string]any {
	return map[string]any{
		"status":         "ok",
		"uptime_seconds": 12.5,
		"ranking":        map[string]any{"model_version": "classifier-v1", "embedding_dim": 1024},
		"embedding": map[string]any{
			"model": ml.Model, "dim": ml.Dim, "mode": "auto", "provider": "local",
			"providers": map[string]any{
				"hf:deepinfra": map[string]any{"status": "error", "detail": "auth: HTTP 403", "latency_ms": nil},
				"local":        map[string]any{"status": "loaded", "detail": "device=cpu threads=8", "latency_ms": 41.5},
			},
			"cache": map[string]any{"enabled": false},
			"stats": map[string]any{"calls": 0},
		},
		"profile_template_version": "profile-v1",
		"jev":                      jev,
	}
}

func ptr[T any](v T) *T { return &v }

// waitFor polls cond for up to two seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
