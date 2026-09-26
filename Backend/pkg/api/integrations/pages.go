package integrations

import (
	"Backend/pkg/httpx"
	"Backend/pkg/store"
	"bytes"
	"errors"
	"html/template"
	"net/http"

	"github.com/rs/zerolog/log"
)

// Hosted pages open in the app's web sheet (ASWebAuthenticationSession),
// which closes when the page sends the browser to sidequestz://…. They are
// plain server-rendered HTML: no scripts, nothing loaded from elsewhere.

// paymentsDone is where the card page sends the browser when a card is saved.
const paymentsDone = "sidequestz://payments/done"

// maxFormBytes caps the card page's form body.
const maxFormBytes = 16 << 10

// page is one hosted page.
type page struct {
	Title string
	Error string
	Body  []string
	Form  *cardForm
	Link  template.URL // back into the app; set by redirect
}

// cardForm is the card page's two forms: a demo card or a test number.
type cardForm struct {
	Action    string
	Token     string
	TestCards []string
}

var pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · SideQuests</title>
<style>
body{margin:0;background:#F2F1EA;color:#18211C;font:16px/1.45 -apple-system,BlinkMacSystemFont,"Helvetica Neue",Arial,sans-serif}
main{max-width:420px;margin:0 auto;padding:40px 20px}
h1{font-size:26px;line-height:1.2;margin:0 0 14px}
p{margin:0 0 12px;color:#3A433D}
.error{background:#FDECEC;color:#991B1B;border:1px solid #E5A3A3;border-radius:10px;padding:10px 12px}
form{margin:20px 0 0}
label{display:block;font-weight:600;margin:0 0 6px}
input{display:block;width:100%;box-sizing:border-box;padding:12px;margin:0 0 10px;border:1px solid #CFD2C8;border-radius:10px;background:#F8F7F2;font-size:16px}
button,.button{display:block;width:100%;box-sizing:border-box;padding:14px;border:0;border-radius:12px;background:#18211C;color:#fff;font-size:16px;font-weight:600;text-align:center;text-decoration:none}
button.secondary{background:#fff;color:#18211C;border:1px solid #CFD2C8}
.or{text-align:center;color:#626A63;font-size:14px;margin:22px 0 0}
.hint{font-size:14px;color:#626A63;margin:14px 0 4px}
ul{margin:0;padding-left:18px;font-size:14px;color:#626A63}
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
{{with .Error}}<p class="error" role="alert">{{.}}</p>{{end}}
{{range .Body}}<p>{{.}}</p>
{{end}}
{{- with .Form}}
<form method="post" action="{{.Action}}">
<input type="hidden" name="t" value="{{.Token}}">
<input type="hidden" name="card" value="demo">
<button type="submit">Use a demo card</button>
</form>
<p class="or">or add a test card</p>
<form method="post" action="{{.Action}}">
<input type="hidden" name="t" value="{{.Token}}">
<input type="hidden" name="card" value="number">
<label for="number">Test card number</label>
<input id="number" name="number" inputmode="numeric" autocomplete="off" placeholder="4242 4242 4242 4242">
<button type="submit" class="secondary">Add test card</button>
</form>
<p class="hint">Test numbers:</p>
<ul>{{range .TestCards}}<li>{{.}}</li>{{end}}</ul>
{{- end}}
{{with .Link}}<p><a class="button" href="{{.}}">Back to SideQuests</a></p>{{end}}
</main>
</body>
</html>
`))

// The pages every flow shares.
var (
	expiredPage = page{
		Title: "This link has expired",
		Body:  []string{"Links like this work once and only for 15 minutes. Close this page and start again in SideQuests."},
	}
	errorPage = page{
		Title: "Something went wrong",
		Body:  []string{"Something went wrong on our side. Close this page and try again."},
	}
	badFormPage = page{
		Title: "That didn't work",
		Body:  []string{"The form didn't come through. Close this page and start again in SideQuests."},
	}
)

// render writes a page with headers that keep it out of caches, frames and
// referrers (its URL carries a one-time token).
func render(w http.ResponseWriter, status int, p page) {
	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, p); err != nil {
		log.Error().Err(err).Msg("render hosted page")
		http.Error(w, "Something went wrong on our side. Try again.", http.StatusInternalServerError)
		return
	}
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		log.Warn().Err(err).Msg("write hosted page")
	}
}

// redirect sends the browser back into the app (302 to a sidequestz:// link)
// with the page as the body for a browser that stays.
func redirect(w http.ResponseWriter, location string, p page) {
	w.Header().Set("Location", location)
	p.Link = template.URL(location) // a fixed sidequestz:// link built here, never user input
	render(w, http.StatusFound, p)
}

// pageFail answers a hosted-page error: an unknown, used or expired link (or
// its account gone) is the expired page, anything else a logged 500.
func pageFail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		render(w, http.StatusBadRequest, expiredPage)
		return
	}
	log.Error().Err(err).Str("request_id", httpx.RequestID(r)).Str("path", r.URL.Path).Msg("hosted page failed")
	render(w, http.StatusInternalServerError, errorPage)
}

// cardPage is the simulated card page for a live link.
func (h *H) cardPage(token, problem string) page {
	hints := make([]string, 0, len(testCards))
	for _, tc := range testCards {
		n := tc.number
		hints = append(hints, n[0:4]+" "+n[4:8]+" "+n[8:12]+" "+n[12:16]+" ("+tc.card.brand+")")
	}
	return page{
		Title: "Add a card",
		Error: problem,
		Body:  []string{"This is a demo card page: SideQuests doesn't charge cards yet. Only a card's brand and last four digits are saved, never the full number."},
		Form:  &cardForm{Action: h.publicURL("/pay/setup"), Token: token, TestCards: hints},
	}
}

// CardPage is GET /pay/setup?t= (public: the link is the credential), the
// simulated card page. It only reads the link; saving a card uses it up.
func (h *H) CardPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	if _, err := h.d.Store.WebSessions().Peek(r.Context(), token, store.PurposePaymentSetup); err != nil {
		pageFail(w, r, err)
		return
	}
	render(w, http.StatusOK, h.cardPage(token, ""))
}

// SubmitCardPage is POST /pay/setup (form: t, card=demo|number, number).
// Simulated: "demo" saves the next demo card (Visa 4242, Mastercard 5454,
// then Visa 1881); "number" accepts only the listed test numbers. It uses up
// the link, saves brand and last four, and 302s to sidequestz://payments/done.
// A wrong number shows the page again without using up the link; the typed
// number is never stored, logged or echoed back.
func (h *H) SubmitCardPage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		render(w, http.StatusBadRequest, badFormPage)
		return
	}
	token := r.PostForm.Get("t")
	ctx := r.Context()
	sessions := h.d.Store.WebSessions()
	sess, err := sessions.Peek(ctx, token, store.PurposePaymentSetup)
	if err != nil {
		pageFail(w, r, err)
		return
	}
	count, err := h.d.Store.Payments().Count(ctx, sess.UserID)
	if err != nil {
		pageFail(w, r, err)
		return
	}
	if count >= maxCards {
		render(w, http.StatusBadRequest, page{Title: "Too many cards", Body: []string{MsgTooManyCards}, Link: paymentsDone})
		return
	}
	chosen := demoCards[min(count, len(demoCards)-1)]
	if r.PostForm.Get("card") == "number" {
		typed, ok := testCard(r.PostForm.Get("number"))
		if !ok {
			render(w, http.StatusBadRequest, h.cardPage(token, "Use one of the test numbers below. This demo page doesn't take real cards."))
			return
		}
		chosen = typed
	}
	if _, err := sessions.Consume(ctx, token, store.PurposePaymentSetup); err != nil {
		pageFail(w, r, err)
		return
	}
	if _, err := h.saveCard(ctx, sess.UserID, chosen); err != nil {
		pageFail(w, r, err)
		return
	}
	redirect(w, paymentsDone, page{
		Title: "Card added",
		Body:  []string{chosen.brand + " •••• " + chosen.last4 + " is saved to your SideQuests account.", "Taking you back to SideQuests…"},
	})
}
