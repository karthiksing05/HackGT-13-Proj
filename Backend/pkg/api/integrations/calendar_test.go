package integrations_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCalendarConnectStartDisconnect(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Cal Person")
	b := srv.Signup(t, "Cal Other")
	list := func(sess *testutil.Session) string {
		t.Helper()
		return canonical(t, srv.Do(t, "GET", "/integrations", nil, sess).Expect(t, http.StatusOK).Body)
	}
	const none = `[{"connected":false,"provider":"google"},{"connected":false,"provider":"outlook"}]`
	const google = `[{"connected":true,"provider":"google"},{"connected":false,"provider":"outlook"}]`
	if got := list(a); got != none {
		t.Fatalf("fresh integrations: %s", got)
	}

	var link contract.URLResponse
	srv.Do(t, "POST", "/integrations/google/connect", nil, a).Expect(t, http.StatusOK).JSON(t, &link)
	path, _ := hostedLink(t, srv, link.URL, "/integrations/google/start")

	// A google link on the outlook page is refused and stays usable.
	wrong := browser(t, srv, "GET", strings.Replace(path, "/google/", "/outlook/", 1), nil)
	if wrong.Status != http.StatusBadRequest || !strings.Contains(string(wrong.Body), "This link has expired") {
		t.Fatalf("link on the other provider: %d %s", wrong.Status, wrong.Body)
	}

	// Public page (no token): marks connected and sends the browser back to the app.
	res := browser(t, srv, "GET", path, nil)
	deepLink := "sidequestz://integrations/google/done?status=connected"
	if res.Status != http.StatusFound || res.Header.Get("Location") != deepLink {
		t.Fatalf("start: %d %v", res.Status, res.Header)
	}
	expectPageHeaders(t, res)
	body := string(res.Body)
	if !strings.Contains(body, "doesn&#39;t read any calendar data") || !strings.Contains(body, `href="`+deepLink+`"`) || !strings.Contains(body, "Google Calendar connected") {
		t.Fatalf("start page must say the sync is simulated and link back: %s", body)
	}
	if got := list(a); got != google {
		t.Fatalf("A after connecting: %s", got)
	}
	if got := list(b); got != none {
		t.Fatalf("B after A connected: %s", got)
	}

	// One use only.
	if res := browser(t, srv, "GET", path, nil); res.Status != http.StatusBadRequest || !strings.Contains(string(res.Body), "This link has expired") {
		t.Fatalf("reused link: %d", res.Status)
	}
	for _, p := range []string{"/integrations/google/start", "/integrations/google/start?t=nope"} {
		if res := browser(t, srv, "GET", p, nil); res.Status != http.StatusBadRequest {
			t.Fatalf("%s: %d", p, res.Status)
		}
	}
	// A card-page link is not a calendar link.
	var cardLink contract.URLResponse
	srv.Do(t, "POST", "/me/payment-methods/setup", nil, a).Expect(t, http.StatusOK).JSON(t, &cardLink)
	_, cardToken := hostedLink(t, srv, cardLink.URL, "/pay/setup")
	if res := browser(t, srv, "GET", "/integrations/google/start?t="+cardToken, nil); res.Status != http.StatusBadRequest {
		t.Fatalf("card link on the calendar page: %d", res.Status)
	}

	// Links expire after 15 minutes.
	srv.Do(t, "POST", "/integrations/outlook/connect", nil, a).Expect(t, http.StatusOK).JSON(t, &link)
	path, _ = hostedLink(t, srv, link.URL, "/integrations/outlook/start")
	srv.Clock.Advance(16 * time.Minute)
	if res := browser(t, srv, "GET", path, nil); res.Status != http.StatusBadRequest {
		t.Fatalf("expired link: %d", res.Status)
	}
	if got := list(a); got != google {
		t.Fatalf("an expired link connected outlook: %s", got)
	}

	// Disconnect is per account.
	srv.Do(t, "DELETE", "/integrations/google", nil, b).Expect(t, http.StatusNoContent)
	if got := list(a); got != google {
		t.Fatalf("B's disconnect reached A: %s", got)
	}
	srv.Do(t, "DELETE", "/integrations/google", nil, a).Expect(t, http.StatusNoContent)
	if got := list(a); got != none {
		t.Fatalf("after disconnect: %s", got)
	}

	for _, r := range []struct{ method, path string }{
		{"GET", "/integrations"}, {"POST", "/integrations/google/connect"}, {"DELETE", "/integrations/outlook"},
	} {
		if res := srv.Do(t, r.method, r.path, nil, nil); res.Status != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", r.method, r.path, res.Status)
		}
	}
	if res := srv.Do(t, "POST", "/integrations/yahoo/connect", nil, a); res.Status != http.StatusNotFound {
		t.Fatalf("unknown provider: %d", res.Status)
	}
}
