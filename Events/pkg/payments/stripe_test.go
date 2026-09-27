package payments

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
