package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// StripeAPIVersion is the preview API version the SPT endpoints need.
const StripeAPIVersion = "2026-04-22.preview"

// ErrDeclined is a token Stripe refused to issue for the card (declined,
// needs authentication): the purchase can't go ahead with this card.
var ErrDeclined = errors.New("payments: the card can't be used for this purchase")

// Stripe issues SPTs with a test-mode secret key. It is a small form client:
// the SPT endpoints are preview-only and not typed in stripe-go.
type Stripe struct {
	key       string
	base      string
	returnURL string
	http      *http.Client
}

// NewStripe builds an issuer for secretKey (sk_test_…) against base (Stripe's
// host outside tests). returnURL is where Stripe sends a buyer after a
// 3-D Secure step.
func NewStripe(secretKey, base, returnURL string) *Stripe {
	if base == "" {
		base = "https://api.stripe.com"
	}
	return &Stripe{key: secretKey, base: strings.TrimRight(base, "/"), returnURL: returnURL, http: &http.Client{Timeout: 20 * time.Second}}
}

type issuedToken struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Issue creates a token for req and returns its id (spt_…).
func (s *Stripe) Issue(ctx context.Context, req IssueRequest) (string, error) {
	form := url.Values{}
	form.Set("payment_method", req.PaymentMethod)
	form.Set("seller_details[network_business_profile]", req.SellerProfile)
	form.Set("usage_limits[currency]", req.Currency)
	form.Set("usage_limits[max_amount]", strconv.Itoa(req.MaxCents))
	form.Set("usage_limits[expires_at]", strconv.FormatInt(req.ExpiresAt.Unix(), 10))
	if s.returnURL != "" {
		form.Set("return_url", s.returnURL)
	}
	var tok issuedToken
	if err := s.post(ctx, "/v1/shared_payment/issued_tokens", form, req.IdempotencyKey, &tok); err != nil {
		return "", err
	}
	if tok.ID == "" {
		return "", errors.New("stripe: issued token without an id")
	}
	if tok.Status == "requires_action" {
		// The sandbox demo has no 3-D Secure step; drop the token.
		_ = s.Revoke(ctx, tok.ID)
		return "", ErrDeclined
	}
	return tok.ID, nil
}

// Revoke deactivates a token so the merchant can no longer charge it.
func (s *Stripe) Revoke(ctx context.Context, spt string) error {
	var tok issuedToken
	return s.post(ctx, "/v1/shared_payment/issued_tokens/"+url.PathEscape(spt)+"/revoke", url.Values{}, "", &tok)
}

func (s *Stripe) post(ctx context.Context, path string, form url.Values, idemKey string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.key, "")
	req.Header.Set("Stripe-Version", StripeAPIVersion)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("stripe %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("stripe %s: read: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		var se struct {
			Error struct {
				Type    string `json:"type"`
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &se)
		if se.Error.Type == "card_error" {
			return fmt.Errorf("%w: %s", ErrDeclined, se.Error.Message)
		}
		// Never include the request (it names the payment method); Stripe's
		// message is enough for the log.
		return fmt.Errorf("stripe %s: %d %s: %s", path, resp.StatusCode, se.Error.Code, se.Error.Message)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("stripe %s: decode: %w", path, err)
	}
	return nil
}
