package api

import (
	"errors"
	"events/pkg/models"
	"events/pkg/payments"
	"events/pkg/store"
	"events/pkg/ui"
	"fmt"
	"html/template"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// The website: listings, event pages, checkout (paid on Stripe Checkout)
// and tickets.

// fewLeft is when an event page starts saying how many tickets remain.
const fewLeft = 15

func html(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
}

// message writes a plain page with one message and a link.
func (d *Deps) message(w http.ResponseWriter, status int, heading, body, linkURL, linkLabel string) {
	html(w, status)
	_ = ui.RenderMessage(w, ui.MessageView{
		Meta:    ui.Meta{Title: heading + " · " + ui.SiteName},
		Heading: heading, Body: body, LinkURL: linkURL, LinkLabel: linkLabel,
	})
}

// HandleNotFound is the site's 404 page.
func (d *Deps) HandleNotFound(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSONError(w, http.StatusNotFound, "not_found", "")
		return
	}
	d.message(w, http.StatusNotFound, "Page not found", "We couldn't find that page. It may have moved, or the event may be over.", "/", "Browse events")
}

// event loads the event behind {slug}, or writes the 404 page.
func (d *Deps) event(w http.ResponseWriter, r *http.Request) *models.Event {
	slug := mux.Vars(r)["slug"]
	if models.IsReservedSlug(slug) {
		d.HandleNotFound(w, r)
		return nil
	}
	event, err := d.Store.GetEvent(r.Context(), slug)
	if err != nil {
		d.HandleNotFound(w, r)
		return nil
	}
	return event
}

// soldOut reports whether nobody can buy tickets for event right now.
func (d *Deps) soldOut(r *http.Request, event *models.Event) bool {
	return event.Remaining <= 0 || d.Store.GetScenario(r.Context()) == models.ScenarioSoldOut
}

// HandleHome renders the listings at / (and /events), filtered by
// ?category= and ?q=.
func (d *Deps) HandleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	search := strings.TrimSpace(r.URL.Query().Get("q"))

	all, err := d.Store.ListEvents(ctx, "", "")
	if err != nil {
		http.Error(w, "failed to list events", http.StatusInternalServerError)
		return
	}
	events := all
	if category != "" || search != "" {
		if events, err = d.Store.ListEvents(ctx, category, search); err != nil {
			http.Error(w, "failed to list events", http.StatusInternalServerError)
			return
		}
	}

	html(w, http.StatusOK)
	_ = ui.RenderHome(w, ui.HomeView{
		Meta: ui.Meta{
			Title:       ui.SiteName + " · Things to do in Saltlight Harbor",
			Description: "Tickets for concerts, comedy, tours, classes and nights out in Saltlight Harbor.",
		},
		Days:       ui.GroupByDay(events),
		Categories: ui.Categories(all, category),
		Category:   category,
		Search:     search,
		Count:      len(events),
	})
}

// HandleEvent renders an event's page at /{slug}.
func (d *Deps) HandleEvent(w http.ResponseWriter, r *http.Request) {
	event := d.event(w, r)
	if event == nil {
		return
	}
	soldOut := d.soldOut(r, event)
	html(w, http.StatusOK)
	_ = ui.RenderEvent(w, ui.EventView{
		Meta: ui.Meta{
			Title:       event.Title + " · " + ui.SiteName,
			Description: event.Summary,
			JSONLD:      template.JS(ui.BuildJSONLDEvent(event, d.Cfg.MerchantBaseURL)),
		},
		Event:   event,
		SoldOut: soldOut,
		FewLeft: !soldOut && event.Remaining <= fewLeft,
	})
}

// checkoutView is the checkout page for event with the form's values.
func (d *Deps) checkoutView(r *http.Request, event *models.Event, qty int, name, email, problem string) ui.CheckoutView {
	maxQty := min(ui.MaxTicketsPerOrder, event.Remaining)
	return ui.CheckoutView{
		Meta: ui.Meta{
			Title:  "Checkout · " + event.Title + " · " + ui.SiteName,
			JSONLD: template.JS(ui.BuildJSONLDEvent(event, d.Cfg.MerchantBaseURL)),
		},
		Event:    event,
		SoldOut:  d.soldOut(r, event) || maxQty < 1,
		MaxQty:   max(maxQty, 1),
		Quantity: min(max(qty, 1), max(maxQty, 1)),
		Name:     name,
		Email:    email,
		Error:    problem,
	}
}

// HandleCheckoutPage renders checkout at GET /{slug}/tickets.
func (d *Deps) HandleCheckoutPage(w http.ResponseWriter, r *http.Request) {
	event := d.event(w, r)
	if event == nil {
		return
	}
	qty, _ := strconv.Atoi(r.URL.Query().Get("quantity"))
	html(w, http.StatusOK)
	_ = ui.RenderCheckout(w, d.checkoutView(r, event, qty, "", "", ""))
}

// HandleStartCheckout takes the checkout form (POST /{slug}/tickets) and
// sends the buyer to Stripe Checkout to pay. Nothing is reserved until the
// payment comes back.
func (d *Deps) HandleStartCheckout(w http.ResponseWriter, r *http.Request) {
	event := d.event(w, r)
	if event == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	qty, _ := strconv.Atoi(r.PostFormValue("quantity"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	email := strings.TrimSpace(r.PostFormValue("email"))

	again := func(status int, problem string) {
		html(w, status)
		_ = ui.RenderCheckout(w, d.checkoutView(r, event, qty, name, email, problem))
	}
	if status, problem := validateCheckout(event, d.soldOut(r, event), qty, name, email); status != 0 {
		again(status, problem)
		return
	}

	base := d.BaseURL(r)
	session, err := d.Checkout.CreateSession(r.Context(), payments.SessionRequest{
		Title:       event.Title,
		UnitCents:   event.UnitCents,
		Quantity:    qty,
		FeesCents:   models.CalculateFees(event.UnitCents, qty),
		Currency:    "usd",
		Email:       email,
		SuccessURL:  base + "/orders/complete?session_id={CHECKOUT_SESSION_ID}",
		CancelURL:   base + "/" + event.Slug + "/tickets?quantity=" + strconv.Itoa(qty),
		Description: fmt.Sprintf("%d × %s", qty, event.Title),
		Metadata: map[string]string{
			"event": event.Slug, "quantity": strconv.Itoa(qty), "name": name,
			"unit_cents": strconv.Itoa(event.UnitCents),
		},
	})
	if err != nil {
		if !errors.Is(err, payments.ErrNotConfigured) {
			log.Error().Err(err).Str("event", event.Slug).Msg("checkout session")
		}
		again(http.StatusServiceUnavailable, "We can't take payments right now. Please try again in a few minutes.")
		return
	}
	http.Redirect(w, r, session.URL, http.StatusSeeOther)
}

func validateCheckout(event *models.Event, soldOut bool, qty int, name, email string) (int, string) {
	switch {
	case soldOut:
		return http.StatusConflict, ""
	case qty < 1 || qty > ui.MaxTicketsPerOrder:
		return http.StatusBadRequest, "Choose how many tickets you want."
	case qty > event.Remaining:
		return http.StatusConflict, fmt.Sprintf("Only %d tickets are left.", event.Remaining)
	case name == "" || len(name) > 100:
		return http.StatusBadRequest, "Enter the name the tickets should be under."
	case !validEmail(email):
		return http.StatusBadRequest, "Enter a valid email address for your tickets."
	default:
		return 0, ""
	}
}

func validEmail(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s && strings.Contains(s[strings.LastIndex(s, "@"):], ".")
}

// HandleCheckoutComplete is where Stripe sends the buyer back
// (GET /orders/complete?session_id=…): once the session is paid it issues the
// ticket (once per session) and shows it.
func (d *Deps) HandleCheckoutComplete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sessionID == "" || len(sessionID) > 255 {
		d.HandleNotFound(w, r)
		return
	}
	idemKey := "checkout:" + sessionID
	if existing, err := d.Store.GetOrderByIdempotencyKey(ctx, idemKey); err == nil && existing != nil {
		http.Redirect(w, r, "/t/"+existing.Ticket.TicketID, http.StatusSeeOther)
		return
	}

	session, err := d.Checkout.GetSession(ctx, sessionID)
	if err != nil {
		log.Warn().Err(err).Msg("checkout session lookup")
		d.message(w, http.StatusNotFound, "We couldn't find that order", "If you were charged, your tickets will be emailed to you. Otherwise, please try again.", "/", "Browse events")
		return
	}
	slug := session.Metadata["event"]
	qty, _ := strconv.Atoi(session.Metadata["quantity"])
	event, err := d.Store.GetEvent(ctx, slug)
	if err != nil || qty < 1 {
		d.message(w, http.StatusNotFound, "We couldn't find that order", "Please contact us if you were charged.", "/", "Browse events")
		return
	}
	if !session.Paid {
		d.message(w, http.StatusPaymentRequired, "Payment not completed", "Your payment didn't go through, so no tickets were issued. You can try again.", "/"+event.Slug+"/tickets", "Back to checkout")
		return
	}

	// Paid: hold the seats. Tickets were available when checkout started; if
	// they ran out meanwhile the buyer still gets the tickets they paid for.
	if err := d.Store.ReserveTickets(ctx, slug, qty); err != nil {
		log.Warn().Err(err).Str("event", slug).Str("session", sessionID).Msg("paid checkout over capacity")
	}
	subtotal := event.UnitCents * qty
	if unit, err := strconv.Atoi(session.Metadata["unit_cents"]); err == nil {
		subtotal = unit * qty
	}
	total := session.AmountTotal
	if total == 0 {
		total = subtotal + models.CalculateFees(subtotal/qty, qty)
	}
	order := d.newConfirmation(r, event.SummaryView(), qty, subtotal, total, "usd",
		models.BuyerInfo{Name: session.Metadata["name"], Email: session.Email},
		models.PaymentSummary{
			Scheme: models.PaymentSchemeStripeCheckout, Brand: session.Brand, Last4: session.Last4,
			PaymentIntentID: session.PaymentIntentID,
		}, idemKey, d.Now().UTC())
	if err := d.Store.SaveOrder(ctx, order, idemKey); err != nil {
		if errors.Is(err, store.ErrDuplicateOrder) {
			_ = d.Store.ReleaseTickets(ctx, slug, qty)
			if existing, gerr := d.Store.GetOrderByIdempotencyKey(ctx, idemKey); gerr == nil {
				http.Redirect(w, r, "/t/"+existing.Ticket.TicketID, http.StatusSeeOther)
				return
			}
		}
		log.Error().Err(err).Str("session", sessionID).Msg("paid checkout not saved")
		d.message(w, http.StatusInternalServerError, "Something went wrong", "Your payment went through but we couldn't show your tickets. Refresh this page to try again.", r.URL.RequestURI(), "Try again")
		return
	}
	http.Redirect(w, r, "/t/"+order.Ticket.TicketID, http.StatusSeeOther)
}

// HandleTicketPass shows a ticket at /t/{ticket_id} (JSON with
// Accept: application/json).
func (d *Deps) HandleTicketPass(w http.ResponseWriter, r *http.Request) {
	wantsJSON := strings.Contains(r.Header.Get("Accept"), "application/json")
	order, err := d.Store.GetOrderByTicketID(r.Context(), mux.Vars(r)["ticket_id"])
	if err != nil {
		if wantsJSON {
			writeJSONError(w, http.StatusNotFound, "not_found", "")
			return
		}
		d.message(w, http.StatusNotFound, "Ticket not found", "Check the link in your confirmation email.", "/", "Browse events")
		return
	}
	if wantsJSON {
		writeJSON(w, http.StatusOK, order.Ticket)
		return
	}

	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; img-src data:; form-action 'self'; base-uri 'none'")
	start, _ := time.Parse(time.RFC3339, order.Event.StartsAt)
	html(w, http.StatusOK)
	_ = ui.RenderTicket(w, ui.TicketView{
		Meta: ui.Meta{
			Title:  "Your tickets · " + order.Event.Title,
			JSONLD: template.JS(ui.BuildJSONLDReservation(order)),
		},
		Order: order,
		Start: start,
		Bars:  ui.Barcode(order.Ticket.Barcode),
	})
}
