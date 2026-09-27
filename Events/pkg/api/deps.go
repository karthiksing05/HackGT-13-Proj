package api

import (
	"events/pkg/config"
	"events/pkg/payments"
	"events/pkg/store"
	"events/pkg/tap"
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
