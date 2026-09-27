package api_test

import (
	"context"
	"events/pkg/payments"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func postForm(h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestPagesServe(t *testing.T) {
	h, _ := setupTestServer(t)
	for path, want := range map[string]string{
		"/":                                 "Sunset Jazz on Pier Nine",
		"/?category=comedy":                 "Seasick Standup",
		"/sunset-jazz-on-pier-nine":         "Get tickets",
		"/sunset-jazz-on-pier-nine/tickets": "Continue to payment",
		"/sunset-jazz-on-pier-nine/tickets?quantity=3": `value="3" selected`,
	} {
		w := get(h, path)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Errorf("GET %s: %d, missing %q", path, w.Code, want)
		}
	}
	if w := get(h, "/no-such-event"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "Page not found") {
		t.Errorf("unknown event: %d", w.Code)
	}
	if w := get(h, "/dashboard"); w.Code != http.StatusNotFound {
		t.Errorf("/dashboard is gone, got %d", w.Code)
	}
}

func TestBrowserCheckoutIssuesOneTicket(t *testing.T) {
	h, deps := setupTestServer(t)
	fake := deps.Checkout.(*payments.Fake)
	ctx := context.Background()
	before, _ := deps.Store.GetEvent(ctx, "sunset-jazz-on-pier-nine")
	remaining := before.Remaining

	w := postForm(h, "/sunset-jazz-on-pier-nine/tickets", url.Values{"quantity": {"2"}, "name": {"Sandy Byte"}, "email": {"sandy@example.com"}})
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "https://checkout.stripe.test/") {
		t.Fatalf("start checkout: %d %s", w.Code, w.Header().Get("Location"))
	}
	id := w.Header().Get("Location")[strings.LastIndex(w.Header().Get("Location"), "/")+1:]

	// Back from Stripe before paying: no ticket.
	if w := get(h, "/orders/complete?session_id="+id); w.Code != http.StatusPaymentRequired {
		t.Fatalf("unpaid: %d", w.Code)
	}
	fake.Pay(id)
	w = get(h, "/orders/complete?session_id="+id)
	ticketPath := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(ticketPath, "/t/") {
		t.Fatalf("complete: %d %s", w.Code, ticketPath)
	}
	// A reload lands on the same ticket and holds no more seats.
	if again := get(h, "/orders/complete?session_id="+id); again.Header().Get("Location") != ticketPath {
		t.Fatalf("reload gave %s, want %s", again.Header().Get("Location"), ticketPath)
	}
	after, _ := deps.Store.GetEvent(ctx, "sunset-jazz-on-pier-nine")
	if after.Remaining != remaining-2 {
		t.Fatalf("remaining %d, want %d", after.Remaining, remaining-2)
	}

	page := get(h, ticketPath)
	// 2 × $12 = $24, fees $1.92 + $1.00 = $2.92, total $26.92.
	for _, want := range []string{"Sunset Jazz on Pier Nine", "Sandy Byte", "Admits", "$26.92", "Visa •••• 4242", "sandy@example.com"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("ticket page missing %q", want)
		}
	}
}

func TestBrowserCheckoutValidates(t *testing.T) {
	h, _ := setupTestServer(t)
	cases := map[string]url.Values{
		"Enter the name":   {"quantity": {"1"}, "name": {""}, "email": {"a@b.co"}},
		"valid email":      {"quantity": {"1"}, "name": {"Sandy"}, "email": {"not-an-email"}},
		"how many tickets": {"quantity": {"0"}, "name": {"Sandy"}, "email": {"a@b.co"}},
	}
	for want, form := range cases {
		w := postForm(h, "/sunset-jazz-on-pier-nine/tickets", form)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%v: %d, want the %q error", form, w.Code, want)
		}
	}
	if w := get(h, "/orders/complete?session_id=cs_unknown"); w.Code != http.StatusNotFound {
		t.Errorf("unknown session: %d", w.Code)
	}
}
