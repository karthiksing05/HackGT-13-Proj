//go:build live

package agent_test

// Live end-to-end runs: real Muse, the real Events merchant and Stripe test
// mode (two sandboxes). Nothing here runs in `go test ./...`; run it with
// Backend/scripts/agent-e2e.sh, which starts Events and sets the env.
//
//   STRIPE_SECRET_KEY      the agent sandbox (issues tokens)
//   STRIPE_SELLER_PROFILE  the merchant sandbox's profile_test_…
//   MUSE_API_KEY           Meta Model API
//   E2E_MERCHANT_BASE_URL  where Events listens (http://localhost:8085)
//   E2E_DEMO_KEY           Events' DEMO_KEY (dev default sqz-booth-demo)

import (
	"Backend/pkg/agent"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"Backend/pkg/tap"
	"Backend/pkg/testutil"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type liveEvent struct {
	slug  string
	title string
	unit  int // the merchant's seed price, for the plan estimate
}

var (
	jazz    = liveEvent{"sunset-jazz-on-pier-nine", "Sunset Jazz on Pier Nine", 1200}
	regatta = liveEvent{"shipyard-robot-regatta", "Shipyard Robot Regatta", 500}
)

type liveRun struct {
	srv    *testutil.Server
	runner *agent.Runner
	sess   *testutil.Session
	itin   *models.Itinerary
	items  []string
}

func liveEnv(t *testing.T) (base, demoKey string) {
	t.Helper()
	for _, k := range []string{"STRIPE_SECRET_KEY", "STRIPE_SELLER_PROFILE", "MUSE_API_KEY", "E2E_MERCHANT_BASE_URL"} {
		if os.Getenv(k) == "" {
			t.Skipf("%s is not set (run Backend/scripts/agent-e2e.sh)", k)
		}
	}
	demoKey = os.Getenv("E2E_DEMO_KEY")
	if demoKey == "" {
		demoKey = "sqz-booth-demo"
	}
	return strings.TrimRight(os.Getenv("E2E_MERCHANT_BASE_URL"), "/"), demoKey
}

// newLiveRun is the harness with the real pieces: Stripe from the config,
// Muse from MUSE_API_KEY, and the merchant client against Events, signing
// with the real clock (the test server's clock is frozen).
func newLiveRun(t *testing.T, base string, events ...liveEvent) *liveRun {
	t.Helper()
	srv := testutil.New(t, testutil.WithConfig(func(c *config.Config) {
		c.PaymentsMode = "sandbox"
		c.StripeSecretKey, c.StripeSellerProfile = os.Getenv("STRIPE_SECRET_KEY"), os.Getenv("STRIPE_SELLER_PROFILE")
		c.MerchantHost, c.MerchantBaseURL = merchantHost, base
		c.MuseAPIKey = os.Getenv("MUSE_API_KEY")
		if m := os.Getenv("MUSE_MODEL"); m != "" {
			c.MuseModel = m
		}
	}))
	_, priv := tap.DefaultKeyPair()
	runner, err := agent.New(context.Background(), srv.Deps, agent.Options{
		Merchant: agent.NewMerchant(base, merchantHost, priv, time.Now),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.Deps.Runner = runner
	lr := &liveRun{srv: srv, runner: runner}
	lr.sess = srv.Signup(t, "Sandy Byte")
	card := &models.PaymentMethod{ID: store.NewID(), UserID: lr.sess.UserID, Brand: "Visa", Last4: "4242", IsDefault: true, CreatedAt: time.Now().UTC()}
	if _, err := srv.Store.Collection(store.CollPaymentMethods).InsertOne(context.Background(), card); err != nil {
		t.Fatal(err)
	}
	start := srv.Clock.Now().UTC().Add(24 * time.Hour).Truncate(time.Minute)
	lr.itin = &models.Itinerary{
		ID: store.NewID(), HostID: lr.sess.UserID, MemberIDs: []string{lr.sess.UserID}, Title: "Saltlight evening",
		DateKey: start.Format("2006-01-02"), TZ: testutil.TimeZone, Date: start.Truncate(24 * time.Hour), Start: start, BackBy: start.Add(6 * time.Hour),
		Visibility: models.VisibilityJustMe, Status: models.ItineraryActive, CreatedAt: start, UpdatedAt: start,
	}
	for i, ev := range events {
		unit := ev.unit
		ticket := "https://" + merchantHost + "/" + ev.slug + "/tickets"
		id := store.NewID()
		lr.items = append(lr.items, id)
		lr.itin.Items = append(lr.itin.Items, models.ItineraryItem{
			ID: id, Kind: models.ItemStop, Title: ev.title, Start: start.Add(time.Duration(i) * time.Hour), End: start.Add(time.Duration(i)*time.Hour + 50*time.Minute),
			Bookable: true, PriceCents: &unit, TicketURL: &ticket,
		})
	}
	if _, err := srv.Store.Collection(store.CollItineraries).InsertOne(context.Background(), lr.itin); err != nil {
		t.Fatal(err)
	}
	return lr
}

// run approves a run for every stop through the API and waits for it.
func (lr *liveRun) run(t *testing.T, budget int) contract.CheckoutRun {
	t.Helper()
	req := contract.CreateCheckoutRun{BudgetCents: budget}
	for _, id := range lr.items {
		req.Items = append(req.Items, contract.CheckoutRunItem{ItemID: id, Quantity: 1})
	}
	var run contract.CheckoutRun
	lr.srv.Do(t, "POST", "/itineraries/"+lr.itin.ID+"/checkout-runs", req, lr.sess).Expect(t, http.StatusCreated).JSON(t, &run)
	lr.runner.Wait()
	var done contract.CheckoutRun
	lr.srv.Do(t, "GET", "/checkout/runs/"+run.ID, nil, lr.sess).Expect(t, http.StatusOK).JSON(t, &done)
	lr.report(t, done)
	return done
}

// report prints what a person checking the run wants to see.
func (lr *liveRun) report(t *testing.T, run contract.CheckoutRun) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "\n  agent=%s state=%s spent=%d of %d\n", deref(run.Agent), run.State, run.SpentCents, run.BudgetCents)
	fmt.Fprintf(&b, "  summary: %s\n", deref(run.Summary))
	for _, in := range run.Intents {
		fmt.Fprintf(&b, "  - %s: %s", in.ItemTitle, in.State)
		if in.Confirmation != nil {
			fmt.Fprintf(&b, " %s", *in.Confirmation)
		}
		if in.FinalCents != nil {
			fmt.Fprintf(&b, " charged=%d max=%d", *in.FinalCents, derefInt(in.MaxAuthorizedCents))
		}
		if in.FailureCode != nil {
			fmt.Fprintf(&b, " failure=%s (%s)", *in.FailureCode, deref(in.FailureReason))
		}
		if in.TicketURL != nil {
			fmt.Fprintf(&b, " ticket=%s", *in.TicketURL)
		}
		b.WriteString("\n")
	}
	stored, err := lr.srv.Store.CheckoutRuns().ByID(context.Background(), run.ID)
	if err == nil {
		b.WriteString("  transcript:\n")
		for _, ev := range stored.Transcript {
			text := strings.ReplaceAll(ev.Text, "\n", " ")
			if len(text) > 160 {
				text = text[:160] + "…"
			}
			fmt.Fprintf(&b, "    %-11s %-12s %s\n", ev.Kind, ev.Name, text)
		}
		if strings.Contains(fmt.Sprint(stored.Transcript), "spt_") {
			t.Errorf("a payment token id reached the transcript")
		}
	}
	t.Log(b.String())
}

func setScenario(t *testing.T, base, key, scenario string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+"/_demo/scenario", bytes.NewReader([]byte(`{"scenario":"`+scenario+`"}`)))
	req.Header.Set("X-Demo-Key", key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set scenario %s: %d", scenario, resp.StatusCode)
	}
}

func deref(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

func expectMuse(t *testing.T, run contract.CheckoutRun) {
	t.Helper()
	if deref(run.Agent) != "muse" {
		t.Errorf("agent = %s: Muse didn't finish this run (the code fallback did)", deref(run.Agent))
	}
}

func states(run contract.CheckoutRun) map[string]string {
	out := map[string]string{}
	for _, in := range run.Intents {
		s := string(in.State)
		if in.FailureCode != nil {
			s += "/" + *in.FailureCode
		}
		out[in.ItemTitle] = s
	}
	return out
}

func TestLiveMuse(t *testing.T) {
	base, key := liveEnv(t)
	setScenario(t, base, key, "normal")
	t.Cleanup(func() { setScenario(t, base, key, "normal") })

	t.Run("buys both within the budget", func(t *testing.T) {
		run := newLiveRun(t, base, jazz, regatta).run(t, 6000)
		expectMuse(t, run)
		for title, s := range states(run) {
			if s != "booked" {
				t.Errorf("%s: %s, want booked", title, s)
			}
		}
	})

	t.Run("budget fits one", func(t *testing.T) {
		// Jazz is $12 + fees = $13.46 and fits $15; the regatta ($5.90) then doesn't.
		run := newLiveRun(t, base, jazz, regatta).run(t, 1500)
		expectMuse(t, run)
		got := states(run)
		if got[jazz.title] != "booked" || !strings.HasPrefix(got[regatta.title], "failed") {
			t.Errorf("states = %v, want jazz booked and the regatta not", got)
		}
		if run.SpentCents > 1500 {
			t.Errorf("spent %d over the $15 budget", run.SpentCents)
		}
	})

	for _, c := range []struct{ scenario, want string }{
		{"sold_out", "failed/sold_out"},
		{"overcharge", "failed/declined"},
	} {
		t.Run(c.scenario, func(t *testing.T) {
			setScenario(t, base, key, c.scenario)
			defer setScenario(t, base, key, "normal")
			run := newLiveRun(t, base, regatta).run(t, 3000)
			expectMuse(t, run)
			if got := states(run)[regatta.title]; got != c.want {
				t.Errorf("regatta: %s, want %s", got, c.want)
			}
		})
	}

	t.Run("price_bump", func(t *testing.T) {
		setScenario(t, base, key, "price_bump")
		defer setScenario(t, base, key, "normal")
		// Plenty of room: Muse should accept the new quote and still buy.
		run := newLiveRun(t, base, regatta).run(t, 5000)
		expectMuse(t, run)
		t.Logf("price_bump outcome: %v", states(run))
	})
}
