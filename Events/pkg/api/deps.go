package api

import (
	"crypto/ed25519"
	"events/pkg/config"
	"events/pkg/store"
	"events/pkg/tap"
	"events/pkg/visa"
	"time"
)

// Deps holds shared dependencies for all merchant HTTP handlers.
type Deps struct {
	Store        store.Store
	Cfg          *config.Config
	KeyDirectory *tap.InMemoryKeyDirectory
	Authorizer   visa.Authorizer
	VisaNet      *visa.SimulatedNetwork
	DemoPubKey   ed25519.PublicKey
	DemoPrivKey  ed25519.PrivateKey
	Now          func() time.Time
}

// NewDeps initializes default handler dependencies.
func NewDeps(cfg *config.Config, st store.Store) *Deps {
	keyDir, pub, priv := tap.NewDefaultKeyDirectory(cfg.TapAgentKey)

	visaNet := visa.NewSimulatedNetwork(cfg.SandboxNetworkKey)
	var auth visa.Authorizer = visaNet
	if cfg.VisaAuthorizeURL != "" {
		auth = visa.NewHTTPClientAuthorizer(cfg.VisaAuthorizeURL, cfg.SandboxNetworkKey)
	}

	return &Deps{
		Store:        st,
		Cfg:          cfg,
		KeyDirectory: keyDir,
		Authorizer:   auth,
		VisaNet:      visaNet,
		DemoPubKey:   pub,
		DemoPrivKey:  priv,
		Now:          time.Now,
	}
}
