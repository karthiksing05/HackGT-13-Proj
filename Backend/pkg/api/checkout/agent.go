package checkout

import (
	"Backend/pkg/api"
	"Backend/pkg/models"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// tickInterval is how often the agent looks for due steps.
const tickInterval = 500 * time.Millisecond

// agentBatch caps the intents advanced in one tick; the rest wait for the next.
const agentBatch = 100

// Agent is the checkout agent: it moves persisted intents through their
// timed steps, preparing → awaiting_approval (the quote is ready) and
// processing → booked (the ticket lands on the buyer's item), and tells the
// owner each time (checkout.status). Its state lives in Mongo, so a restart
// picks up where it left off, and each step is an atomic guarded update, so
// a racing cancel (or a second agent) never moves an intent twice.
//
// Simulated: the agent finds, fills in and pays for nothing. It waits
// CHECKOUT_STEP_DELAY per step and writes a demo ticket.
type Agent struct {
	d        *api.Deps
	interval time.Duration
}

// NewAgent builds an agent over d; call Run (or Tick in tests).
func NewAgent(d *api.Deps) *Agent {
	return &Agent{d: d, interval: tickInterval}
}

// StartAgent runs an agent in the background until ctx ends (main.go).
func StartAgent(ctx context.Context, d *api.Deps) *Agent {
	a := NewAgent(d)
	go a.Run(ctx)
	return a
}

// Run ticks until ctx ends. A failing tick is logged and retried on the next.
func (a *Agent) Run(ctx context.Context) {
	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	for {
		if _, err := a.Tick(ctx); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("checkout agent tick")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick advances every intent whose step is due now and reports how many
// moved. Steps fall due by the real clock; what they stamp on the intent
// and the ticket is in the owner's own time (a demo account's demo date).
func (a *Agent) Tick(ctx context.Context) (int, error) {
	now := a.d.Clock()
	due, err := a.d.Store.CheckoutIntents().Due(ctx, now, agentBatch)
	if err != nil {
		return 0, fmt.Errorf("due intents: %w", err)
	}
	moved := 0
	var errs []error
	for _, intent := range due {
		next, ok, err := a.step(a.d.ForUser(ctx, intent.UserID), intent, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("intent %s: %w", intent.ID, err))
			continue
		}
		if ok {
			moved++
			publish(a.d, next)
		}
	}
	return moved, errors.Join(errs...)
}

// step performs the intent's next timed transition. ok is false when the
// intent moved on meanwhile (cancelled, or advanced by another agent).
func (a *Agent) step(ctx context.Context, intent *models.CheckoutIntent, now time.Time) (*models.CheckoutIntent, bool, error) {
	intents := a.d.Store.CheckoutIntents()
	switch intent.State {
	case models.CheckoutPreparing:
		// The quote is ready: the first steps are done, approval is left.
		quote(intent)
		set := bson.M{"state": models.CheckoutAwaitingApproval, "steps": quotedSteps()}
		if intent.TotalCents != nil {
			set["feesCents"], set["totalCents"] = *intent.FeesCents, *intent.TotalCents
		}
		return intents.Advance(ctx, intent.ID, models.CheckoutPreparing, now, set)
	case models.CheckoutProcessing:
		set := bson.M{"state": models.CheckoutBooked, "steps": doneSteps(intent.Steps)}
		if intent.TicketID == "" {
			// Every way into processing assigns the ticket; this only
			// repairs a document written without one.
			intent.TicketID, intent.Confirmation = newTicketID(), newConfirmation()
			set["ticketId"], set["confirmation"] = intent.TicketID, intent.Confirmation
		}
		// The ticket goes onto the buyer's item first, so a crash between
		// the two writes is repaired by the next tick instead of losing it.
		if err := a.d.Store.CheckoutReads().SaveTicket(ctx, intent.UserID, intent.ItineraryID, intent.ItemID, a.ticket(intent)); err != nil {
			return nil, false, fmt.Errorf("save ticket: %w", err)
		}
		return intents.Advance(ctx, intent.ID, models.CheckoutProcessing, now, set)
	}
	return nil, false, nil
}

// ticket is what the booking leaves on the buyer's item.
func (a *Agent) ticket(intent *models.CheckoutIntent) models.ItemTicket {
	total := intent.TotalCents
	if total == nil {
		total = intent.SubtotalCents
	}
	confirmation := intent.Confirmation
	url := ticketURL(a.d.Cfg.PublicBaseURL, intent.TicketID)
	return models.ItemTicket{ID: intent.TicketID, Quantity: intent.Quantity, TotalCents: total, Confirmation: &confirmation, URL: &url}
}
