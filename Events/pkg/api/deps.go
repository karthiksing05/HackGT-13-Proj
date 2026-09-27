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
	Now          func() time.Time
}

// NewDeps wires the handlers: the agent's public key (the demo key only in
// dev) and the Stripe charger (unconfigured without a key).
func NewDeps(cfg *config.Config, st store.Store) (*Deps, error) {
	keyDir, err := tap.NewDirectory(cfg.TapAgentPublicKey, cfg.IsDev())
	if err != nil {
		return nil, err
	}
	var charger payments.Charger = payments.Unconfigured{}
	if cfg.StripeSecretKey != "" {
		charger = payments.NewStripe(cfg.StripeSecretKey, cfg.StripeAPIBase)
	}
	return &Deps{
		Store:        st,
		Cfg:          cfg,
		KeyDirectory: keyDir,
		Charger:      charger,
		Now:          time.Now,
	}, nil
}

// demoKeyOK reports whether the request carries the booth's demo key.
func (d *Deps) demoKeyOK(key string) bool {
	return key != "" && key == d.Cfg.DemoKey
}
