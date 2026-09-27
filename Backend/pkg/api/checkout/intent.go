package checkout

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/util"
	"crypto/rand"
	"strings"
)

// maxQuantity caps the tickets in one checkout.
const maxQuantity = 10

// The agent's progress lines (the contract example has the quoted set).
// While it prepares, the lines read as work in progress; once the quote is
// ready the first two are done and approval is what is left.
const (
	stepFinding  = "Finding tickets on the official site"
	stepFound    = "Found tickets on the official site"
	stepFilling  = "Filling in your name and email"
	stepFilled   = "Filled in your name and email"
	stepApproval = "Waiting for your approval"
	stepInstant  = "Paying instantly (within your limit)"
)

func preparingSteps() []models.CheckoutStep {
	return []models.CheckoutStep{{Text: stepFinding}, {Text: stepFilling}, {Text: stepApproval}}
}

func quotedSteps() []models.CheckoutStep {
	return []models.CheckoutStep{{Text: stepFound, Done: true}, {Text: stepFilled, Done: true}, {Text: stepApproval}}
}

func instantSteps() []models.CheckoutStep {
	return []models.CheckoutStep{{Text: stepFound, Done: true}, {Text: stepFilled, Done: true}, {Text: stepInstant}}
}

// doneSteps checks every line off (booked).
func doneSteps(steps []models.CheckoutStep) []models.CheckoutStep {
	out := make([]models.CheckoutStep, len(steps))
	for i, step := range steps {
		out[i] = models.CheckoutStep{Text: step.Text, Done: true}
	}
	return out
}

// quote fills fees and total once the price is known. Simulated: the agent
// adds no fees, so the total is the subtotal; an unknown price stays unknown.
func quote(intent *models.CheckoutIntent) {
	if intent.SubtotalCents == nil {
		return
	}
	fees, total := 0, *intent.SubtotalCents
	intent.FeesCents, intent.TotalCents = &fees, &total
}

// pickCard is the card the agent pays with: the one asked for, else the
// default, else the first saved (the app sends null for "my default").
func pickCard(cards []*models.PaymentMethod, requested *string) *models.PaymentMethod {
	if requested != nil {
		for _, card := range cards {
			if card.ID == *requested {
				return card
			}
		}
	}
	for _, card := range cards {
		if card.IsDefault {
			return card
		}
	}
	if len(cards) > 0 {
		return cards[0]
	}
	return nil
}

// render is the wire shape of an intent (the owner is the only viewer).
func render(intent *models.CheckoutIntent) contract.CheckoutIntent {
	steps := make([]contract.CheckoutStep, 0, len(intent.Steps))
	for _, step := range intent.Steps {
		steps = append(steps, contract.CheckoutStep{Text: step.Text, Done: step.Done})
	}
	return contract.CheckoutIntent{
		ID:                 intent.ID,
		ItemID:             intent.ItemID,
		ItemTitle:          intent.ItemTitle,
		Steps:              steps,
		SubtotalCents:      intent.SubtotalCents,
		FeesCents:          intent.FeesCents,
		TotalCents:         intent.TotalCents,
		CardBrand:          intent.CardBrand,
		CardLast4:          intent.CardLast4,
		State:              contract.CheckoutState(intent.State),
		Quantity:           intent.Quantity,
		PaymentMethodID:    intent.PaymentMethodID,
		FailureReason:      intent.FailureReason,
		Instant:            intent.Instant,
		RunID:              optional(intent.RunID),
		Merchant:           optional(intent.Merchant),
		CheckoutURL:        optional(intent.CheckoutURL),
		MaxAuthorizedCents: intent.MaxAuthorizedCents,
		FinalCents:         intent.FinalCents,
		FailureCode:        optional(intent.FailureCode),
		OrderRef:           optional(intent.OrderRef),
		Confirmation:       optionalIf(intent.RunID != "", intent.Confirmation),
		TicketURL:          optional(intent.TicketURL),
	}
}

// optional is s as a pointer, nil when empty.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// optionalIf is optional(s) when cond holds.
func optionalIf(cond bool, s string) *string {
	if !cond {
		return nil
	}
	return optional(s)
}

// publish tells the owner's devices about the intent's state (checkout.status).
func publish(d *api.Deps, intent *models.CheckoutIntent) {
	realtime.CheckoutStatus(d.Publish(), intent.UserID, intent.ID, contract.CheckoutState(intent.State))
}

// newTicketID is the ticket's public id: 128 random bits, since the ticket
// page needs nothing else to open.
func newTicketID() string { return util.RandomToken(16) }

// confirmationAlphabet is Crockford's base32 (no I, L, O or U to misread).
const confirmationAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newConfirmation is a booking code like "SQ-4F7K2".
func newConfirmation() string {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		panic("checkout: crypto/rand failed: " + err.Error())
	}
	for i := range b {
		b[i] = confirmationAlphabet[int(b[i])%len(confirmationAlphabet)]
	}
	return "SQ-" + string(b)
}

// ticketURL is where the booked ticket opens (GET /tickets/{id}).
func ticketURL(publicBaseURL, ticketID string) string {
	return strings.TrimRight(publicBaseURL, "/") + "/tickets/" + ticketID
}
