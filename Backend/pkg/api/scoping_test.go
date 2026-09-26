package api_test

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// scope is one resource created by user A that user B must not reach. Area
// agents append rows as their endpoints land (backend-contract §6,
// scoping_test): a 404 for unowned resources, 403 for host-only edits by a
// member.
type scope struct {
	name   string
	setup  func(t *testing.T, srv *testutil.Server, a *testutil.Session) (method, path string, body any)
	status int // expected for B
}

var scopes = []scope{
	{"GET /itineraries/{id} of A", createItineraryForA, http.StatusNotFound},
	{"PATCH /itineraries/{id} by a member", joinAsMemberThenPatch, http.StatusForbidden},
	// backend-C: {"GET /threads/{id} of A", startDMForA, 404},
	// backend-D: {"GET /checkout/intents/{id} of A", createIntentForA, 404},
	{"DELETE /me/payment-methods/{id} of A", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
		var card contract.PaymentMethod
		srv.Do(t, "POST", "/me/payment-methods", contract.AddPaymentMethod{Token: "tok_visa_4242"}, a).Expect(t, http.StatusCreated).JSON(t, &card)
		return "DELETE", "/me/payment-methods/" + card.ID, nil
	}, http.StatusNotFound},
	{"DELETE /me/devices/{token} of A", func(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
		srv.Do(t, "POST", "/me/devices", contract.DeviceRegistration{PushToken: "a0b1c2d3e4f5a6b7", Platform: "ios"}, a).Expect(t, http.StatusNoContent)
		return "DELETE", "/me/devices/a0b1c2d3e4f5a6b7", nil
	}, http.StatusNotFound},
}

func TestScoping(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Alice Scope")
	b := srv.Signup(t, "Bob Scope")

	// A's token reads A; B's reads B; neither reads the other.
	var me contract.User
	srv.Do(t, "GET", "/me", nil, a).Expect(t, http.StatusOK).JSON(t, &me)
	if me.ID != a.UserID || me.ID == b.UserID {
		t.Fatalf("A's token returned %s, want %s", me.ID, a.UserID)
	}
	srv.Do(t, "GET", "/me", nil, b).Expect(t, http.StatusOK).JSON(t, &me)
	if me.ID != b.UserID {
		t.Fatalf("B's token returned %s, want %s", me.ID, b.UserID)
	}
	// B's refresh token never yields A's session.
	var rotated contract.RefreshResponse
	srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: b.Refresh}, nil).Expect(t, http.StatusOK).JSON(t, &rotated)
	srv.Do(t, "GET", "/me", nil, &testutil.Session{Access: rotated.AccessToken}).Expect(t, http.StatusOK).JSON(t, &me)
	if me.ID != b.UserID {
		t.Fatalf("rotated B token returned %s", me.ID)
	}

	for _, sc := range scopes {
		t.Run(sc.name, func(t *testing.T) {
			method, path, body := sc.setup(t, srv, a)
			if res := srv.Do(t, method, path, body, b); res.Status != sc.status {
				t.Fatalf("B got %d for A's resource, want %d: %s", res.Status, sc.status, res.Body)
			}
		})
	}
}

// TestStubsAnswer501 pins the placeholder shape every unbuilt route answers with.
func TestStubsAnswer501(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Stub Person")
	res := stubProbe(t, srv, a)
	if res.Status != http.StatusNotImplemented || res.Message() != "Not built yet" {
		t.Fatalf("stub: %d %s", res.Status, res.Body)
	}
	if res := stubProbe(t, srv, nil); res.Status != http.StatusUnauthorized {
		t.Fatalf("protected stub without a token: %d", res.Status)
	}
	// A public stub needs no token (on its own router: the real ones get built).
	public := mux.NewRouter()
	api.Stub(public, srv.Deps, "GET", "/public-stub", false)
	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, httptest.NewRequest("GET", "/public-stub", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("public stub: %d", rec.Code)
	}
	if res := srv.Do(t, "POST", "/plans/generate", contract.PlanRequest{Range: "transit", Ride: "none", Who: "friends", Pace: "balanced", Budget: 1}, a); res.Status != http.StatusServiceUnavailable || res.Message() != "Planning is warming up. Try again in a moment." {
		t.Fatalf("planning without a planner: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "GET", "/nowhere", nil, nil); res.Status != http.StatusNotFound || res.Message() == "" {
		t.Fatalf("404 shape: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "GET", "/healthz", nil, nil); res.Status != http.StatusOK || string(res.Body) != "{\"ok\":true}\n" {
		t.Fatalf("healthz: %d %s", res.Status, res.Body)
	}
}
