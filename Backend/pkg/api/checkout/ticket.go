package checkout

import (
	"Backend/pkg/httpx"
	"Backend/pkg/store"
	"bytes"
	"errors"
	"html/template"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// ticketPage is the whole ticket: what, when, where, the confirmation code
// and how many. Simulated: it says plainly that nothing was bought.
var ticketPage = template.Must(template.New("ticket").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{if .Found}}Ticket · {{.Title}}{{else}}Ticket not found{{end}} · SideQuests</title>
<style>
body{margin:0;padding:24px 16px;background:#F4EFE6;color:#18211C;font:16px/1.45 -apple-system,BlinkMacSystemFont,"Helvetica Neue",Arial,sans-serif}
main{max-width:420px;margin:0 auto;background:#fff;border-radius:18px;padding:24px;box-shadow:0 1px 3px rgba(24,33,28,.12)}
.brand{margin:0;font-weight:700;letter-spacing:.02em}
.badge{display:inline-block;margin:10px 0 0;padding:2px 10px;border-radius:999px;background:#E7ECE6;color:#4A5F49;font-size:13px;font-weight:600}
h1{margin:14px 0 4px;font-size:24px;line-height:1.2}
.sub{margin:0;color:#5B6660}
dl{margin:20px 0 0;border-top:1px dashed #C9C2B4;padding-top:16px;display:grid;grid-template-columns:auto 1fr;gap:8px 16px}
dt{color:#5B6660}
dd{margin:0;text-align:right;font-variant-numeric:tabular-nums}
.code{font-size:20px;font-weight:700;letter-spacing:.06em}
.note{margin:20px 0 0;font-size:13px;color:#5B6660}
</style>
</head>
<body>
<main>
<p class="brand">SideQuests</p>
{{if .Found -}}
<p class="badge">Demo ticket</p>
<h1>{{.Title}}</h1>
{{if .When}}<p class="sub">{{.When}}</p>{{end}}
{{if .Where}}<p class="sub">{{.Where}}</p>{{end}}
<dl>
<dt>Confirmation</dt><dd class="code">{{.Confirmation}}</dd>
<dt>Tickets</dt><dd>{{.Quantity}}</dd>
{{if .Total}}<dt>Total</dt><dd>{{.Total}}</dd>{{end}}
{{if .Card}}<dt>Card</dt><dd>{{.Card}}</dd>{{end}}
</dl>
<p class="note">Booked by the SideQuests checkout agent, which is a demo: nothing was bought or charged, and this ticket isn't valid for entry.</p>
{{- else -}}
<h1>Ticket not found</h1>
<p class="sub">This ticket doesn't exist, or its checkout never went through.</p>
{{- end}}
</main>
</body>
</html>
`))

// ticketView is what the page shows; empty strings are left out.
type ticketView struct {
	Found        bool
	Title        string
	When         string
	Where        string
	Confirmation string
	Quantity     int
	Total        string
	Card         string
}

// Ticket is GET /tickets/{id}: a booked ticket as a small public page. The
// ticket id is unguessable and is the only thing needed to open it.
func (h *H) Ticket(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	intent, err := h.d.Store.CheckoutIntents().ByTicket(ctx, mux.Vars(r)["id"])
	if errors.Is(err, store.ErrNotFound) {
		writeTicket(w, r, http.StatusNotFound, ticketView{})
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	view := ticketView{
		Found:        true,
		Title:        intent.ItemTitle,
		Confirmation: intent.Confirmation,
		Quantity:     intent.Quantity,
	}
	total := intent.TotalCents
	if total == nil {
		total = intent.SubtotalCents
	}
	if total != nil {
		view.Total = httpx.DollarsFixed(*total)
	}
	if intent.CardLast4 != "" {
		view.Card = intent.CardBrand + " •••• " + intent.CardLast4
	}
	// The plan may have been deleted since; the ticket still shows what was booked.
	itin, item, err := h.d.Store.CheckoutReads().Item(ctx, intent.ItineraryID, intent.ItemID)
	switch {
	case err == nil:
		loc := httpx.Location(itin.TZ)
		start, end := item.Start.In(loc), item.End.In(loc)
		view.When = start.Format("Mon, Jan 2") + " · " + httpx.TimeRange(start, end)
		if item.Place != nil {
			view.Where = item.Place.Name
		}
	case !errors.Is(err, store.ErrNotFound):
		log.Warn().Err(err).Str("intent", intent.ID).Msg("ticket page: item lookup")
	}
	writeTicket(w, r, http.StatusOK, view)
}

// writeTicket renders the page with headers that keep the capability URL
// out of caches, search engines and referrers.
func writeTicket(w http.ResponseWriter, r *http.Request, status int, view ticketView) {
	var buf bytes.Buffer
	if err := ticketPage.Execute(&buf, view); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Warn().Err(err).Msg("write ticket page")
	}
}
