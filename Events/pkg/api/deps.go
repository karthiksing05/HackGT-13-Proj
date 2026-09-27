package api

import (
	"events/pkg/config"
	"events/pkg/payments"
	"events/pkg/store"
	"events/pkg/tap"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// MerchantID is this merchant's id in quotes and payment metadata.
const MerchantID = "sidequestz-events"

// Deps holds shared dependencies for all merchant HTTP handlers.
type Deps struct {
	Store        store.Store
	Cfg          *config.Config
	KeyDirectory *tap.InMemoryKeyDirectory
	Charger      payments.Charger
	Checkout     payments.Checkout // browser purchases (Stripe Checkout)
	Now          func() time.Time
}

// NewDeps wires the handlers: the agent's public key (the demo key only in
// dev), and Stripe for SPT charges and browser checkouts (unconfigured
// without a key).
func NewDeps(cfg *config.Config, st store.Store) (*Deps, error) {
	keyDir, err := tap.NewDirectory(cfg.TapAgentPublicKey, cfg.IsDev())
	if err != nil {
		return nil, err
	}
	var charger payments.Charger = payments.Unconfigured{}
	var checkout payments.Checkout = payments.Unconfigured{}
	if cfg.StripeSecretKey != "" {
		stripe := payments.NewStripe(cfg.StripeSecretKey, cfg.StripeAPIBase)
		charger, checkout = stripe, stripe
	}
	return &Deps{
		Store:        st,
		Cfg:          cfg,
		KeyDirectory: keyDir,
		Charger:      charger,
		Checkout:     checkout,
		Now:          time.Now,
	}, nil
}

// demoKeyOK reports whether the request carries the booth's demo key.
func (d *Deps) demoKeyOK(key string) bool {
	return key != "" && key == d.Cfg.DemoKey
}

// BaseURL returns the public base URL. If r is provided and contains host/proxy
// headers, it infers the origin scheme and host dynamically so that Stripe Checkout
// redirects and ticket URLs always lead back to the website the user is actually visiting
// (e.g. https://events.sidequestz.tech), while still supporting localhost during local testing.
func (d *Deps) BaseURL(r *http.Request) string {
	if r != nil {
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
		if host != "" {
			proto := "http"
			if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
				proto = p
			} else if r.TLS != nil {
				proto = "https"
			} else if !strings.HasPrefix(host, "localhost") && !strings.HasPrefix(host, "127.0.0.1") {
				proto = "https"
			}
			return fmt.Sprintf("%s://%s", proto, host)
		}
	}
	if d != nil && d.Cfg != nil && d.Cfg.MerchantBaseURL != "" && !strings.Contains(d.Cfg.MerchantBaseURL, "localhost") && !strings.Contains(d.Cfg.MerchantBaseURL, "127.0.0.1") {
		return strings.TrimRight(d.Cfg.MerchantBaseURL, "/")
	}
	if d != nil && d.Cfg != nil && d.Cfg.MerchantHost != "" && !strings.Contains(d.Cfg.MerchantHost, "localhost") {
		return "https://" + d.Cfg.MerchantHost
	}
	if d != nil && d.Cfg != nil && d.Cfg.MerchantBaseURL != "" {
		return strings.TrimRight(d.Cfg.MerchantBaseURL, "/")
	}
	return "https://events.sidequestz.tech"
}
