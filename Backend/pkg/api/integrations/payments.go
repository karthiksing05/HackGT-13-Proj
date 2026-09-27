package integrations

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
)

// Simulated: saved cards are a brand and the last four digits. Nothing is
// charged, and a card number (only ever one of the listed test numbers) is
// never stored, logged or sent back.

// maxCards caps the cards one account can save.
const maxCards = 10

// Sentences for saved cards.
const (
	MsgCardToken    = "Add the card from the card page in SideQuests."
	MsgTooManyCards = "You can save up to 10 cards. Remove one in SideQuests first."
)

// card is a simulated card: display brand and last four digits.
type card struct {
	brand string
	last4 string
}

// demoToken is the stored stand-in for a payment-SDK token (tok_visa_4242).
func (c card) demoToken() string {
	return "tok_" + strings.ToLower(strings.ReplaceAll(c.brand, " ", "")) + "_" + c.last4
}

// demoCards are what "Use a demo card" adds, in turn (the app's demo cards).
// They are Stripe's test cards, so agentic checkout pays with the matching
// Stripe test PaymentMethod (payments.TestPaymentMethod) and the last four
// digits match what Stripe reports.
var demoCards = []card{{"Visa", "4242"}, {"Mastercard", "4444"}, {"Visa", "5556"}}

// testCards are the only numbers the card page accepts (Stripe test numbers),
// shown on the page as hints. Visa 0002 always declines.
var testCards = []struct {
	number string
	card   card
	note   string
}{
	{"4242424242424242", demoCards[0], ""},
	{"5555555555554444", demoCards[1], ""},
	{"4000056655665556", demoCards[2], "debit"},
	{"4000000000000002", card{"Visa", "0002"}, "always declines"},
}

// testCard looks up a typed number, ignoring spaces and dashes.
func testCard(typed string) (card, bool) {
	number := strings.NewReplacer(" ", "", "-", "").Replace(strings.TrimSpace(typed))
	for _, tc := range testCards {
		if tc.number == number {
			return tc.card, true
		}
	}
	return card{}, false
}

// cardBrands maps the brand in a tok_<brand>_<last4> token to its name.
var cardBrands = map[string]string{
	"visa":            "Visa",
	"mastercard":      "Mastercard",
	"mc":              "Mastercard",
	"amex":            "Amex",
	"americanexpress": "Amex",
	"discover":        "Discover",
	"jcb":             "JCB",
	"diners":          "Diners Club",
	"unionpay":        "UnionPay",
}

var cardTokenPattern = regexp.MustCompile(`^tok_([a-z]+)_([0-9]{4})$`)

// parseCardToken reads tok_<brand>_<last4> (tok_mastercard_5454); any other
// token is the Visa 4242 test card.
func parseCardToken(token string) card {
	if m := cardTokenPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(token))); m != nil {
		if brand, ok := cardBrands[m[1]]; ok {
			return card{brand: brand, last4: m[2]}
		}
	}
	return demoCards[0]
}

// ListCards is GET /me/payment-methods → [PaymentMethod], oldest first.
func (h *H) ListCards(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	cards, err := h.d.Store.Payments().List(r.Context(), user.ID.Hex())
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, cardsView(cards))
}

// CardSetup is POST /me/payment-methods/setup → {url} of the hosted card
// page, a one-time link for the caller that works for pageTTL.
func (h *H) CardSetup(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	token, err := h.d.Store.WebSessions().Create(r.Context(), user.ID.Hex(), store.PurposePaymentSetup, "", pageTTL)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, contract.URLResponse{URL: h.publicURL("/pay/setup?t=" + url.QueryEscape(token))})
}

// AddCard is POST /me/payment-methods {token} → 201 PaymentMethod. The token
// is tok_<brand>_<last4> from a payment sheet; anything else saves the Visa
// 4242 test card. The first card is the default.
func (h *H) AddCard(w http.ResponseWriter, r *http.Request) {
	var req contract.AddPaymentMethod
	if !httpx.Decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		httpx.Error(w, http.StatusBadRequest, MsgCardToken)
		return
	}
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	userID := user.ID.Hex()
	count, err := h.d.Store.Payments().Count(r.Context(), userID)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if count >= maxCards {
		httpx.Error(w, http.StatusBadRequest, MsgTooManyCards)
		return
	}
	saved, err := h.saveCard(r.Context(), userID, parseCardToken(req.Token))
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, cardView(saved))
}

// DeleteCard is DELETE /me/payment-methods/{id} → 204; deleting the default
// promotes the oldest card left. Another account's card is 404.
func (h *H) DeleteCard(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Payments().Delete(r.Context(), user.ID.Hex(), mux.Vars(r)["id"]); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *H) saveCard(ctx context.Context, userID string, c card) (*models.PaymentMethod, error) {
	return h.d.Store.Payments().Add(ctx, userID, c.brand, c.last4, c.demoToken())
}

func cardView(pm *models.PaymentMethod) contract.PaymentMethod {
	return contract.PaymentMethod{ID: pm.ID, Brand: pm.Brand, Last4: pm.Last4, IsDefault: pm.IsDefault}
}

// cardsView renders a list; when no card is flagged default (data written
// around the store), the first card shows as the default, which is also the
// card checkout falls back to.
func cardsView(cards []*models.PaymentMethod) []contract.PaymentMethod {
	out := make([]contract.PaymentMethod, 0, len(cards))
	hasDefault := false
	for _, pm := range cards {
		out = append(out, cardView(pm))
		hasDefault = hasDefault || pm.IsDefault
	}
	if !hasDefault && len(out) > 0 {
		out[0].IsDefault = true
	}
	return out
}
