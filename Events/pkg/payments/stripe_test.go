package payments

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeStripe answers the two calls Charge makes; charge decides the
// PaymentIntent response.
func fakeStripe(t *testing.T, charge func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, _, ok := r.BasicAuth(); !ok || u != "sk_test_123" {
			t.Errorf("auth: want the secret key as basic-auth user")
		}
		if got := r.Header.Get("Stripe-Version"); got != StripeAPIVersion {
			t.Errorf("Stripe-Version = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/shared_payment/granted_tokens/spt_missing"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","code":"resource_missing","message":"No such shared payment token"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/shared_payment/granted_tokens/"):
			_, _ = w.Write([]byte(`{"id":"spt_1","usage_limits":{"currency":"usd","expires_at":1798761600,"max_amount":2692}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/payment_intents":
			charge(w, r)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestStripeChargeSucceeds(t *testing.T) {
	srv := fakeStripe(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("amount") != "2692" || r.Form.Get("currency") != "usd" || r.Form.Get("confirm") != "true" {
			t.Errorf("form = %v", r.Form)
		}
		if r.Form.Get("payment_method_data[shared_payment_granted_token]") != "spt_1" {
			t.Errorf("token not sent as payment_method_data: %v", r.Form)
		}
		if r.Header.Get("Idempotency-Key") != "order-abc" {
			t.Errorf("Idempotency-Key = %q", r.Header.Get("Idempotency-Key"))
		}
		_, _ = w.Write([]byte(`{"id":"pi_1","status":"succeeded","payment_method":{"card":{"brand":"visa","last4":"4242"}}}`))
	})
	defer srv.Close()

	res, err := NewStripe("sk_test_123", srv.URL).Charge(context.Background(), ChargeRequest{
		SPT: "spt_1", AmountCents: 2692, Currency: "usd", IdempotencyKey: "order-abc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.PaymentIntentID != "pi_1" || res.Brand != "visa" || res.Last4 != "4242" || res.LimitCents != 2692 {
		t.Fatalf("result = %+v", res)
	}
}

func TestStripeDeclines(t *testing.T) {
	cases := []struct {
		name, spt, body string
		status          int
		want            string
	}{
		{"over max_amount", "spt_1", `{"error":{"type":"invalid_request_error","code":"amount_too_large","message":"The amount exceeds the shared payment token's max_amount."}}`, 400, ReasonOverLimit},
		{"expired", "spt_1", `{"error":{"type":"invalid_request_error","code":"shared_payment_token_expired","message":"This token has expired."}}`, 400, ReasonExpired},
		{"used", "spt_1", `{"error":{"type":"invalid_request_error","code":"shared_payment_token_deactivated","message":"This shared payment token has been deactivated."}}`, 400, ReasonUsed},
		// The exact answers Stripe test mode gave (no code, message only).
		{"over the limit (live wording)", "spt_1", `{"error":{"type":"invalid_request_error","message":"The requested amount is greater than the remaining amount capturable with this shared payment granted token."}}`, 400, ReasonOverLimit},
		{"spent (live wording)", "spt_1", `{"error":{"type":"invalid_request_error","message":"The shared payment granted token cannot be used because it is already in a deactivated state."}}`, 400, ReasonUsed},
		{"card declined", "spt_1", `{"error":{"type":"card_error","code":"card_declined","decline_code":"generic_decline","message":"Your card was declined."}}`, 402, ReasonCardDeclined},
		{"unknown token", "spt_missing", ``, 0, ReasonUnknownToken},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeStripe(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(c.body))
			})
			defer srv.Close()
			_, err := NewStripe("sk_test_123", srv.URL).Charge(context.Background(), ChargeRequest{SPT: c.spt, AmountCents: 100, Currency: "usd"})
			var d *DeclineError
			if !errors.As(err, &d) || d.Reason != c.want {
				t.Fatalf("err = %v, want decline %s", err, c.want)
			}
		})
	}
}

func TestStripeAuthErrorIsNotADecline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"Invalid API Key provided"}}`))
	}))
	defer srv.Close()
	_, err := NewStripe("sk_test_123", srv.URL).Charge(context.Background(), ChargeRequest{SPT: "spt_1", AmountCents: 100, Currency: "usd"})
	var d *DeclineError
	if err == nil || errors.As(err, &d) {
		t.Fatalf("err = %v, want a plain error", err)
	}
}

func TestFakeAppliesTokenLimits(t *testing.T) {
	now := time.Date(2026, 9, 27, 18, 0, 0, 0, time.UTC)
	f := NewFake(func() time.Time { return now })
	f.Grant("spt_ok", FakeToken{MaxCents: 1000})
	f.Grant("spt_old", FakeToken{MaxCents: 1000, ExpiresAt: now.Add(-time.Second)})
	ctx := context.Background()

	if _, err := f.Charge(ctx, ChargeRequest{SPT: "spt_ok", AmountCents: 1001}); !isReason(err, ReasonOverLimit) {
		t.Fatalf("over limit: %v", err)
	}
	if _, err := f.Charge(ctx, ChargeRequest{SPT: "spt_ok", AmountCents: 1000, IdempotencyKey: "k"}); err != nil {
		t.Fatalf("within limit: %v", err)
	}
	if _, err := f.Charge(ctx, ChargeRequest{SPT: "spt_ok", AmountCents: 1000, IdempotencyKey: "k"}); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if _, err := f.Charge(ctx, ChargeRequest{SPT: "spt_ok", AmountCents: 1000}); !isReason(err, ReasonUsed) {
		t.Fatalf("reuse: %v", err)
	}
	if _, err := f.Charge(ctx, ChargeRequest{SPT: "spt_old", AmountCents: 10}); !isReason(err, ReasonExpired) {
		t.Fatalf("expired: %v", err)
	}
}

func isReason(err error, reason string) bool {
	var d *DeclineError
	return errors.As(err, &d) && d.Reason == reason
}

func TestStripeCheckoutSession(t *testing.T) {
	var form url.Values
	var version, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version, path = r.Header.Get("Stripe-Version"), r.URL.RequestURI()
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			form = r.PostForm
			_, _ = w.Write([]byte(`{"id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1","payment_status":"unpaid"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"cs_test_1","payment_status":"paid","amount_total":2692,"metadata":{"event":"x"},
			"customer_details":{"email":"a@b.co"},"payment_intent":{"id":"pi_1","payment_method":{"card":{"brand":"visa","last4":"4242"}}}}`))
	}))
	defer srv.Close()
	s := NewStripe("sk_test_x", srv.URL)

	sess, err := s.CreateSession(context.Background(), SessionRequest{Title: "Jazz", UnitCents: 1200, Quantity: 2, FeesCents: 292,
		Currency: "usd", Email: "a@b.co", SuccessURL: "https://x/ok?session_id={CHECKOUT_SESSION_ID}", CancelURL: "https://x/back",
		Metadata: map[string]string{"event": "x"}})
	if err != nil || sess.URL == "" || sess.Paid {
		t.Fatalf("create: %+v %v", sess, err)
	}
	if version != "" {
		t.Errorf("checkout should use the account's API version, sent %q", version)
	}
	for k, want := range map[string]string{
		"mode": "payment", "line_items[0][price_data][unit_amount]": "1200", "line_items[0][quantity]": "2",
		"line_items[1][price_data][unit_amount]": "292", "line_items[1][price_data][product_data][name]": "Service fee",
		"customer_email": "a@b.co", "metadata[event]": "x",
	} {
		if form.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, form.Get(k), want)
		}
	}

	got, err := s.GetSession(context.Background(), "cs_test_1")
	if err != nil || !got.Paid || got.AmountTotal != 2692 || got.Last4 != "4242" || got.Email != "a@b.co" || got.PaymentIntentID != "pi_1" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if !strings.Contains(path, "expand[]=payment_intent.payment_method") && !strings.Contains(path, "expand%5B%5D=payment_intent.payment_method") {
		t.Errorf("get path %s doesn't expand the card", path)
	}
}
