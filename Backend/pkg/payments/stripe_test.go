package payments

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTestPaymentMethod(t *testing.T) {
	for _, c := range []struct{ brand, last4, want string }{
		{"Visa", "4242", "pm_card_visa"},
		{"Mastercard", "4444", "pm_card_mastercard"},
		{"Visa", "5556", "pm_card_visa_debit"},
		{"Visa", "0002", "pm_card_chargeDeclined"},
		{"Mastercard", "5454", "pm_card_mastercard"}, // an older demo card: by brand
		{"Diners Club", "9999", "pm_card_visa"},
	} {
		if got := TestPaymentMethod(c.brand, c.last4); got != c.want {
			t.Errorf("%s %s → %s, want %s", c.brand, c.last4, got, c.want)
		}
	}
}

func TestStripeIssueSendsTheLimits(t *testing.T) {
	expires := time.Date(2026, 9, 27, 18, 10, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, _, _ := r.BasicAuth(); u != "sk_test_1" || r.Header.Get("Stripe-Version") != StripeAPIVersion {
			t.Errorf("auth or version missing")
		}
		_ = r.ParseForm()
		want := map[string]string{
			"payment_method": "pm_card_visa",
			"seller_details[network_business_profile]": "profile_test_seller",
			"usage_limits[currency]":                   "usd",
			"usage_limits[max_amount]":                 "2692",
			"usage_limits[expires_at]":                 "1790532600",
		}
		for k, v := range want {
			if got := r.Form.Get(k); got != v {
				t.Errorf("%s = %q, want %q", k, got, v)
			}
		}
		if r.Form.Has("return_url") {
			t.Errorf("return_url sent; Stripe rejects it as parameter_unknown")
		}
		if r.Header.Get("Idempotency-Key") != "spt-intent-1" {
			t.Errorf("Idempotency-Key = %q", r.Header.Get("Idempotency-Key"))
		}
		_, _ = w.Write([]byte(`{"id":"spt_123","object":"shared_payment.issued_token","status":"active"}`))
	}))
	defer srv.Close()

	spt, err := NewStripe("sk_test_1", srv.URL).Issue(context.Background(), IssueRequest{
		PaymentMethod: "pm_card_visa", SellerProfile: "profile_test_seller", MaxCents: 2692, Currency: "usd",
		ExpiresAt: expires, IdempotencyKey: "spt-intent-1",
	})
	if err != nil || spt != "spt_123" {
		t.Fatalf("Issue = %q, %v", spt, err)
	}
}

func TestStripeIssueCardErrorIsADecline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"type":"card_error","code":"card_declined","message":"Your card was declined."}}`))
	}))
	defer srv.Close()
	_, err := NewStripe("sk_test_1", srv.URL).Issue(context.Background(), IssueRequest{PaymentMethod: "pm_card_chargeDeclined"})
	if !errors.Is(err, ErrDeclined) {
		t.Fatalf("err = %v, want ErrDeclined", err)
	}
}

func TestStripeRevoke(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"id":"spt_123","status":"deactivated"}`))
	}))
	defer srv.Close()
	if err := NewStripe("sk_test_1", srv.URL).Revoke(context.Background(), "spt_123"); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/shared_payment/issued_tokens/spt_123/revoke" {
		t.Fatalf("path = %s", path)
	}
}
