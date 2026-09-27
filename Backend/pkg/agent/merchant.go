package agent

import (
	"Backend/pkg/tap"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Merchant talks to the sandbox ticket merchant (Events/): every request is
// signed with the agent's TAP key, and only MERCHANT_HOST is ever contacted.
type Merchant struct {
	base  string // MERCHANT_BASE_URL (where requests go)
	host  string // MERCHANT_HOST (the authority the signature covers)
	key   ed25519.PrivateKey
	keyID string
	http  *http.Client
	now   func() time.Time
}

// NewMerchant builds the client.
func NewMerchant(baseURL, host string, key ed25519.PrivateKey, now func() time.Time) *Merchant {
	if now == nil {
		now = time.Now
	}
	return &Merchant{
		base: strings.TrimRight(baseURL, "/"), host: strings.ToLower(host), key: key, keyID: tap.DefaultDemoAgentKeyID,
		http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		now:  now,
	}
}

// Quote is the merchant's offer (GET /api/events/{slug}/offer).
type Quote struct {
	QuoteID        string `json:"quote_id"`
	QuoteExpiresAt string `json:"quote_expires_at"`
	Quantity       int    `json:"quantity"`
	Available      int    `json:"available"`
	Currency       string `json:"currency"`
	UnitCents      int    `json:"unit_cents"`
	SubtotalCents  int    `json:"subtotal_cents"`
	FeesCents      int    `json:"fees_cents"`
	TotalCents     int    `json:"total_cents"`
	Event          struct {
		Slug     string `json:"slug"`
		Title    string `json:"title"`
		StartsAt string `json:"starts_at"`
		Venue    string `json:"venue"`
	} `json:"event"`
}

// OrderRequest is POST /api/orders.
type OrderRequest struct {
	QuoteID            string `json:"quote_id"`
	Quantity           int    `json:"quantity"`
	ExpectedTotalCents int    `json:"expected_total_cents"`
	Payment            struct {
		Scheme string `json:"scheme"`
		Token  string `json:"token"`
	} `json:"payment"`
	Buyer struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"buyer"`
}

// Confirmation is the merchant's order confirmation (201, or 200 on a replay).
type Confirmation struct {
	OrderID          string `json:"order_id"`
	ConfirmationCode string `json:"confirmation_code"`
	Status           string `json:"status"`
	Quantity         int    `json:"quantity"`
	Currency         string `json:"currency"`
	SubtotalCents    int    `json:"subtotal_cents"`
	FeesCents        int    `json:"fees_cents"`
	TotalCents       int    `json:"total_cents"`
	Ticket           struct {
		TicketID  string `json:"ticket_id"`
		TicketURL string `json:"ticket_url"`
		Admit     int    `json:"admit"`
		Barcode   string `json:"barcode"`
	} `json:"ticket"`
	Payment struct {
		Brand      string `json:"brand"`
		Last4      string `json:"last4"`
		LimitCents int    `json:"limit_cents"`
	} `json:"payment"`
}

// MerchantError is an error answer from the merchant (the shared contract).
type MerchantError struct {
	Status        int
	Code          string `json:"code"`
	Message       string `json:"message"`
	DeclineReason string `json:"decline_reason"`
	TotalCents    int    `json:"total_cents"`
	QuoteID       string `json:"quote_id"`
}

func (e *MerchantError) Error() string {
	return fmt.Sprintf("merchant %d %s %s", e.Status, e.Code, e.DeclineReason)
}

// Page is what the agent sees of a merchant page.
type Page struct {
	URL           string            `json:"url"`
	Title         string            `json:"title"`
	Text          string            `json:"untrusted_page_text"`
	JSONLD        []json.RawMessage `json:"json_ld,omitempty"`
	AgentCheckout string            `json:"agent_checkout,omitempty"`
}

// ErrNotMerchant is a URL outside MERCHANT_HOST.
var ErrNotMerchant = errors.New("agent: that URL is not on the ticket merchant")

// maxPageText caps the page text handed to the model.
const maxPageText = 8 << 10

// resolve maps a merchant URL (https://events.sidequestz.tech/x/tickets) to
// where requests go (MERCHANT_BASE_URL + path), refusing any other host.
func (m *Merchant) resolve(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Hostname(), m.host) {
		return "", ErrNotMerchant
	}
	return m.base + u.EscapedPath(), nil
}

// Slug is the event slug of a ticket URL (/{slug}/tickets or /{slug}).
func Slug(ticketURL string) string {
	u, err := url.Parse(ticketURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// Page fetches and reads a merchant page.
func (m *Merchant) Page(ctx context.Context, rawURL string) (*Page, error) {
	target, err := m.resolve(rawURL)
	if err != nil {
		return nil, err
	}
	body, status, err := m.do(ctx, http.MethodGet, target, nil, "agent-browser-auth", "", "text/html")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, &MerchantError{Status: status, Code: "not_found"}
	}
	return readPage(rawURL, string(body)), nil
}

// Offer asks for a quote.
func (m *Merchant) Offer(ctx context.Context, slug string, quantity int) (*Quote, error) {
	target := fmt.Sprintf("%s/api/events/%s/offer?quantity=%d", m.base, url.PathEscape(slug), quantity)
	var q Quote
	if err := m.json(ctx, http.MethodGet, target, nil, "agent-browser-auth", "", &q); err != nil {
		return nil, err
	}
	return &q, nil
}

// Order places an order; idemKey makes a retry return the same order.
func (m *Merchant) Order(ctx context.Context, idemKey string, req OrderRequest) (*Confirmation, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var c Confirmation
	if err := m.json(ctx, http.MethodPost, m.base+"/api/orders", body, "agent-payer-auth", idemKey, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (m *Merchant) json(ctx context.Context, method, target string, body []byte, tag, idemKey string, out any) error {
	raw, status, err := m.do(ctx, method, target, body, tag, idemKey, "application/json")
	if err != nil {
		return err
	}
	if status >= 300 {
		merr := &MerchantError{Status: status}
		_ = json.Unmarshal(raw, merr)
		if merr.Code == "" {
			merr.Code = "merchant_error"
		}
		return merr
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("merchant: decode: %w", err)
	}
	return nil
}

func (m *Merchant) do(ctx context.Context, method, target string, body []byte, tag, idemKey, accept string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	if err := tap.Sign(req, m.key, m.keyID, tag, m.host, m.now().UTC()); err != nil {
		return nil, 0, err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("merchant: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	if err != nil {
		return nil, 0, fmt.Errorf("merchant: read: %w", err)
	}
	return raw, resp.StatusCode, nil
}

var (
	reTitle    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reJSONLD   = regexp.MustCompile(`(?is)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	reCheckout = regexp.MustCompile(`(?is)<link[^>]*rel=["']agent-checkout["'][^>]*>`)
	reHref     = regexp.MustCompile(`(?is)href=["']([^"']+)["']`)
	reDrop     = regexp.MustCompile(`(?is)<(script|style|head)[^>]*>.*?</(script|style|head)>`)
	reTag      = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace    = regexp.MustCompile(`\s+`)
)

// readPage extracts what the model needs from our own merchant's HTML.
func readPage(pageURL, doc string) *Page {
	p := &Page{URL: pageURL}
	if m := reTitle.FindStringSubmatch(doc); m != nil {
		p.Title = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	for _, m := range reJSONLD.FindAllStringSubmatch(doc, 4) {
		if raw := strings.TrimSpace(m[1]); json.Valid([]byte(raw)) {
			p.JSONLD = append(p.JSONLD, json.RawMessage(raw))
		}
	}
	if link := reCheckout.FindString(doc); link != "" {
		if h := reHref.FindStringSubmatch(link); h != nil {
			p.AgentCheckout = html.UnescapeString(h[1])
		}
	}
	text := reDrop.ReplaceAllString(doc, " ")
	text = html.UnescapeString(reTag.ReplaceAllString(text, " "))
	text = strings.TrimSpace(reSpace.ReplaceAllString(text, " "))
	if len(text) > maxPageText {
		text = text[:maxPageText]
	}
	p.Text = text
	return p
}
