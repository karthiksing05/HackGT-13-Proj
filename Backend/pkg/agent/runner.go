// Package agent is the checkout agent for agentic checkout runs. For each
// run it lets Muse Spark (Meta Model API, function calling) open the ticket
// pages from the catalog and buy each approved item from the sandbox
// merchant, through tools that enforce everything in code:
//
//   - only the run's items, in the approved quantities, from MERCHANT_HOST;
//   - the run's budget, reserved atomically before every payment
//     (store.CheckoutRuns.Reserve), so the total can never go over it;
//   - one Stripe Shared Payment Token per purchase, limited to the quoted
//     total and ten minutes, which Muse never sees.
//
// Without a Muse key, or when Muse fails, the same tools run in a fixed
// order (the fallback), so a run always finishes.
//
// Simulated: the merchant is our own sandbox (Events/) and payments are in
// Stripe test mode; no real tickets are bought and nothing is charged.
package agent

import (
	"Backend/pkg/api"
	"Backend/pkg/muse"
	"Backend/pkg/payments"
	"Backend/pkg/tap"
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Model is the part of the Muse client the runner uses (fakes in tests).
type Model interface {
	Create(ctx context.Context, req muse.Request) (*muse.Response, error)
}

// leaseFor is how long a claimed run stays ours without renewal.
const leaseFor = 90 * time.Second

// Runner works checkout runs.
type Runner struct {
	d        *api.Deps
	issuer   payments.Issuer
	merchant *Merchant
	model    Model // nil = fallback only
	seller   string
	maxTurns int
	timeout  time.Duration
	base     context.Context
	wg       sync.WaitGroup
}

// Options are the runner's collaborators; New fills them from the config.
type Options struct {
	Issuer   payments.Issuer
	Merchant *Merchant
	Model    Model
}

// New builds a runner over d from its config: the Stripe issuer, the signed
// merchant client and (with MUSE_API_KEY) Muse. opts override any of them.
func New(ctx context.Context, d *api.Deps, opts Options) (*Runner, error) {
	cfg := d.Cfg
	r := &Runner{
		d: d, issuer: opts.Issuer, merchant: opts.Merchant, model: opts.Model, seller: cfg.StripeSellerProfile,
		maxTurns: cfg.AgentMaxTurns, timeout: cfg.CheckoutRunTimeout, base: ctx,
	}
	if r.maxTurns <= 0 {
		r.maxTurns = 24
	}
	if r.timeout <= 0 {
		r.timeout = 3 * time.Minute
	}
	if r.issuer == nil {
		r.issuer = payments.NewStripe(cfg.StripeSecretKey, cfg.StripeAPIBase)
	}
	if r.merchant == nil {
		key, err := signingKey(cfg.TapAgentKey, cfg.Dev())
		if err != nil {
			return nil, err
		}
		r.merchant = NewMerchant(cfg.MerchantBaseURL, cfg.MerchantHost, key, d.Clock)
	}
	if r.model == nil && cfg.MuseAPIKey != "" {
		r.model = muse.New(cfg.MuseAPIKey, cfg.MuseBaseURL, cfg.MuseModel)
	}
	return r, nil
}

// signingKey is TAP_AGENT_KEY, or the demo key in dev (Events trusts it only in dev).
func signingKey(seed string, dev bool) (ed25519.PrivateKey, error) {
	if seed != "" {
		return tap.PrivateKeyFromSeed(seed)
	}
	if !dev {
		return nil, errors.New("TAP_AGENT_KEY is required outside dev")
	}
	_, priv := tap.DefaultKeyPair()
	return priv, nil
}

// Start works a run in the background (api.CheckoutRunner).
func (r *Runner) Start(runID string) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ctx, cancel := context.WithTimeout(r.base, r.timeout)
		defer cancel()
		if err := r.Run(ctx, runID); err != nil {
			log.Error().Err(err).Str("run", runID).Msg("checkout run")
		}
	}()
}

// Wait blocks until every started run returned (tests, shutdown).
func (r *Runner) Wait() { r.wg.Wait() }

// Resume restarts runs a previous process left running.
func (r *Runner) Resume(ctx context.Context) {
	ids, err := r.d.Store.CheckoutRuns().Stale(ctx, r.d.Clock())
	if err != nil {
		log.Warn().Err(err).Msg("checkout runs: resume")
		return
	}
	for _, id := range ids {
		log.Info().Str("run", id).Msg("resuming checkout run")
		r.Start(id)
	}
}

// Run works one run to the end: it claims the run, lets Muse (or the
// fallback) buy each item, fails whatever is left and writes the summary.
func (r *Runner) Run(ctx context.Context, runID string) error {
	runs := r.d.Store.CheckoutRuns()
	now := r.d.Clock()
	run, ok, err := runs.Claim(ctx, runID, now, now.Add(leaseFor))
	if err != nil || !ok {
		return err // taken by another runner, or finished
	}
	stopLease := r.keepLease(ctx, runID)
	defer stopLease()

	st, err := r.newRunState(ctx, run)
	if err != nil {
		return err
	}
	agent := "fallback"
	if r.model != nil {
		agent = "muse"
	}
	_ = runs.SetAgent(ctx, runID, agent)
	if r.model != nil {
		if err := st.museLoop(ctx); err != nil {
			st.log(ctx, "note", "fallback", "Muse stopped: "+errorText(err))
			log.Warn().Err(err).Str("run", runID).Msg("muse loop; finishing with the fallback")
			_ = runs.SetAgent(ctx, runID, "fallback")
		}
	}
	st.fallback(ctx)
	st.failLeftovers(ctx)
	return st.finish(ctx)
}

// keepLease renews the run's lease until the returned stop is called.
func (r *Runner) keepLease(ctx context.Context, runID string) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(leaseFor / 3)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				_ = r.d.Store.CheckoutRuns().Extend(context.WithoutCancel(ctx), runID, r.d.Clock().Add(leaseFor))
			}
		}
	}()
	return func() { close(done) }
}

// errorText is an error for the transcript: short, never a credential.
func errorText(err error) string {
	s := err.Error()
	if i := strings.Index(s, "spt_"); i >= 0 {
		s = s[:i] + "spt_…"
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// money is "$26.92".
func money(cents int) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/100, cents%100)
}
