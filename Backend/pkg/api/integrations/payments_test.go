package integrations_test

import (
	"Backend/pkg/api/integrations"
	"Backend/pkg/contract"
	"Backend/pkg/store"
	"Backend/pkg/testutil"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func cards(t *testing.T, srv *testutil.Server, sess *testutil.Session) []contract.PaymentMethod {
	t.Helper()
	var out []contract.PaymentMethod
	res := srv.Do(t, "GET", "/me/payment-methods", nil, sess).Expect(t, http.StatusOK)
	if strings.TrimSpace(string(res.Body)) == "null" {
		t.Fatal("the card list must be [] when empty, never null")
	}
	res.JSON(t, &out)
	return out
}

func addCard(t *testing.T, srv *testutil.Server, sess *testutil.Session, token string) contract.PaymentMethod {
	t.Helper()
	var pm contract.PaymentMethod
	srv.Do(t, "POST", "/me/payment-methods", contract.AddPaymentMethod{Token: token}, sess).Expect(t, http.StatusCreated).JSON(t, &pm)
	return pm
}

func TestPaymentMethodsAPI(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Card Person")
	b := srv.Signup(t, "Card Other")
	if got := cards(t, srv, a); len(got) != 0 {
		t.Fatalf("fresh cards: %+v", got)
	}

	visa := addCard(t, srv, a, "tok_visa_4242")
	if visa.ID == "" || visa.Brand != "Visa" || visa.Last4 != "4242" || !visa.IsDefault {
		t.Fatalf("first card: %+v", visa)
	}
	mc := addCard(t, srv, a, "tok_MC_5454")
	fallback := addCard(t, srv, a, "tok_demo")
	amex := addCard(t, srv, a, "  tok_amex_0005 ")
	unknown := addCard(t, srv, a, "tok_foo_1234")
	for _, tc := range []struct {
		got          contract.PaymentMethod
		brand, last4 string
	}{{mc, "Mastercard", "5454"}, {fallback, "Visa", "4242"}, {amex, "Amex", "0005"}, {unknown, "Visa", "4242"}} {
		if tc.got.Brand != tc.brand || tc.got.Last4 != tc.last4 || tc.got.IsDefault {
			t.Errorf("parsed %+v, want %s %s (not default)", tc.got, tc.brand, tc.last4)
		}
	}
	if res := srv.Do(t, "POST", "/me/payment-methods", contract.AddPaymentMethod{Token: " "}, a); res.Status != http.StatusBadRequest || res.Message() != integrations.MsgCardToken {
		t.Fatalf("empty token: %d %s", res.Status, res.Body)
	}
	got := cards(t, srv, a)
	if len(got) != 5 || got[0].ID != visa.ID || got[4].ID != unknown.ID || !got[0].IsDefault || got[1].IsDefault {
		t.Fatalf("list: %+v", got)
	}

	// B sees none of A's cards and can't delete them.
	if got := cards(t, srv, b); len(got) != 0 {
		t.Fatalf("B sees A's cards: %+v", got)
	}
	if res := srv.Do(t, "DELETE", "/me/payment-methods/"+visa.ID, nil, b); res.Status != http.StatusNotFound {
		t.Fatalf("B deleting A's card: %d", res.Status)
	}

	// Deleting a spare keeps the default; deleting the default promotes the oldest left.
	srv.Do(t, "DELETE", "/me/payment-methods/"+mc.ID, nil, a).Expect(t, http.StatusNoContent)
	if got := cards(t, srv, a); len(got) != 4 || got[0].ID != visa.ID || !got[0].IsDefault {
		t.Fatalf("after deleting a spare: %+v", got)
	}
	srv.Do(t, "DELETE", "/me/payment-methods/"+visa.ID, nil, a).Expect(t, http.StatusNoContent)
	got = cards(t, srv, a)
	if len(got) != 3 || got[0].ID != fallback.ID || !got[0].IsDefault || got[1].IsDefault || got[2].IsDefault {
		t.Fatalf("after deleting the default: %+v", got)
	}
	if res := srv.Do(t, "DELETE", "/me/payment-methods/"+visa.ID, nil, a); res.Status != http.StatusNotFound {
		t.Fatalf("delete twice: %d", res.Status)
	}

	// Ten cards at most.
	for len(cards(t, srv, a)) < 10 {
		addCard(t, srv, a, "tok_visa_4242")
	}
	if res := srv.Do(t, "POST", "/me/payment-methods", contract.AddPaymentMethod{Token: "tok_visa_4242"}, a); res.Status != http.StatusBadRequest || res.Message() != integrations.MsgTooManyCards {
		t.Fatalf("eleventh card: %d %s", res.Status, res.Body)
	}

	for _, r := range []struct{ method, path string }{
		{"GET", "/me/payment-methods"}, {"POST", "/me/payment-methods"}, {"POST", "/me/payment-methods/setup"}, {"DELETE", "/me/payment-methods/" + amex.ID},
	} {
		if res := srv.Do(t, r.method, r.path, nil, nil); res.Status != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d", r.method, r.path, res.Status)
		}
	}
}

func TestCardSetupPage(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Page Person")
	b := srv.Signup(t, "Page Other")
	newLink := func() (string, string) {
		t.Helper()
		var link contract.URLResponse
		srv.Do(t, "POST", "/me/payment-methods/setup", nil, a).Expect(t, http.StatusOK).JSON(t, &link)
		return hostedLink(t, srv, link.URL, "/pay/setup")
	}
	submit := func(token string, form url.Values) *testutil.Response {
		t.Helper()
		form.Set("t", token)
		return browser(t, srv, "POST", "/pay/setup", form)
	}

	// The page: two forms carrying the link token, the test numbers, no scripts.
	path, token := newLink()
	res := browser(t, srv, "GET", path, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("card page: %d %s", res.Status, res.Body)
	}
	expectPageHeaders(t, res)
	page := string(res.Body)
	for _, want := range []string{
		"Use a demo card", `action="http://api.test/pay/setup"`, `name="t" value="` + token + `"`,
		"4242 4242 4242 4242 (Visa)", "5555 5555 5555 4444 (Mastercard)", "4000 0566 5566 5556 (Visa, debit)",
		"4000 0000 0000 0002 (Visa, always declines)",
		"never the full number",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("card page lacks %q", want)
		}
	}
	if strings.Contains(page, "<script") {
		t.Error("the card page must not run scripts")
	}
	// Looking at the page does not use the link up.
	browser(t, srv, "GET", path, nil).Expect(t, http.StatusOK)

	// "Use a demo card": Visa 4242, the default, then back into the app.
	res = submit(token, url.Values{"card": {"demo"}})
	if res.Status != http.StatusFound || res.Header.Get("Location") != "sidequestz://payments/done" || !strings.Contains(string(res.Body), "Visa •••• 4242") {
		t.Fatalf("demo card: %d %v %s", res.Status, res.Header, res.Body)
	}
	expectPageHeaders(t, res)
	if got := cards(t, srv, a); len(got) != 1 || got[0].Brand != "Visa" || got[0].Last4 != "4242" || !got[0].IsDefault {
		t.Fatalf("after the demo card: %+v", got)
	}
	// Used up.
	if res := browser(t, srv, "GET", path, nil); res.Status != http.StatusBadRequest || !strings.Contains(string(res.Body), "This link has expired") {
		t.Fatalf("used link, GET: %d", res.Status)
	}
	if res := submit(token, url.Values{"card": {"demo"}}); res.Status != http.StatusBadRequest {
		t.Fatalf("used link, POST: %d", res.Status)
	}

	// A number that isn't a test number shows the page again, never echoes
	// it and keeps the link; a listed test number then works.
	_, token = newLink()
	for _, typed := range []string{"4111 1111 1111 1111", ""} {
		res = submit(token, url.Values{"card": {"number"}, "number": {typed}})
		if res.Status != http.StatusBadRequest || !strings.Contains(string(res.Body), "Use one of the test numbers below") {
			t.Fatalf("number %q: %d %s", typed, res.Status, res.Body)
		}
		if strings.Contains(string(res.Body), "4111") {
			t.Fatal("the typed number was echoed back")
		}
	}
	res = submit(token, url.Values{"card": {"number"}, "number": {" 5555-5555 5555-4444 "}})
	if res.Status != http.StatusFound || !strings.Contains(string(res.Body), "Mastercard •••• 4444") {
		t.Fatalf("test number: %d %s", res.Status, res.Body)
	}

	// Demo cards take turns: the third card is Visa 5556, and so is every one after.
	for range 2 {
		_, token = newLink()
		submit(token, url.Values{"card": {"demo"}}).Expect(t, http.StatusFound)
	}
	got := cards(t, srv, a)
	if len(got) != 4 || got[1].Last4 != "4444" || got[2].Last4 != "5556" || got[3].Last4 != "5556" || got[3].Brand != "Visa" {
		t.Fatalf("demo rotation: %+v", got)
	}

	// Only brand and last four are stored: no card number anywhere.
	cursor, err := srv.Store.Collection(store.CollPaymentMethods).Find(context.Background(), bson.M{"userId": a.UserID})
	if err != nil {
		t.Fatal(err)
	}
	var docs []bson.M
	if err := cursor.All(context.Background(), &docs); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(docs)
	for _, number := range []string{"5555555555554444", "4242424242424242", "4000056655665556", "4111"} {
		if strings.Contains(string(raw), number) {
			t.Fatalf("a card number was stored: %s", raw)
		}
	}

	// The link belongs to A: B has no cards.
	if got := cards(t, srv, b); len(got) != 0 {
		t.Fatalf("B got cards: %+v", got)
	}

	// Unknown, missing, expired and wrong-purpose links.
	for _, p := range []string{"/pay/setup", "/pay/setup?t=nope"} {
		if res := browser(t, srv, "GET", p, nil); res.Status != http.StatusBadRequest {
			t.Fatalf("%s: %d", p, res.Status)
		}
	}
	if res := submit("nope", url.Values{"card": {"demo"}}); res.Status != http.StatusBadRequest {
		t.Fatalf("POST with an unknown token: %d", res.Status)
	}
	var calLink contract.URLResponse
	srv.Do(t, "POST", "/integrations/google/connect", nil, a).Expect(t, http.StatusOK).JSON(t, &calLink)
	_, calToken := hostedLink(t, srv, calLink.URL, "/integrations/google/start")
	if res := submit(calToken, url.Values{"card": {"demo"}}); res.Status != http.StatusBadRequest {
		t.Fatalf("calendar link on the card page: %d", res.Status)
	}
	_, token = newLink()
	srv.Clock.Advance(16 * time.Minute)
	if res := submit(token, url.Values{"card": {"demo"}}); res.Status != http.StatusBadRequest {
		t.Fatalf("expired link: %d", res.Status)
	}
	if got := cards(t, srv, a); len(got) != 4 {
		t.Fatalf("refused submissions added cards: %+v", got)
	}

	// At ten cards the page says so and keeps the link.
	_, token = newLink()
	for len(cards(t, srv, a)) < 10 {
		addCard(t, srv, a, "tok_visa_4242")
	}
	res = submit(token, url.Values{"card": {"demo"}})
	if res.Status != http.StatusBadRequest || !strings.Contains(string(res.Body), "You can save up to 10 cards") || !strings.Contains(string(res.Body), `href="sidequestz://payments/done"`) {
		t.Fatalf("card page at the cap: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "DELETE", "/me/payment-methods/"+cards(t, srv, a)[9].ID, nil, a).Expect(t, http.StatusNoContent)
	submit(token, url.Values{"card": {"demo"}}).Expect(t, http.StatusFound)

	// An oversized form is refused.
	big := url.Values{"card": {"number"}, "number": {strings.Repeat("4", 20<<10)}}
	if res := submit(token, big); res.Status != http.StatusBadRequest {
		t.Fatalf("oversized form: %d", res.Status)
	}
}
