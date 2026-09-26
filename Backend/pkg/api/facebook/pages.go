package facebook

import (
	"Backend/pkg/httpx"
	"Backend/pkg/store"
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// maxCodeLength bounds the confirmation codes looked up (ours are 43 chars).
const maxCodeLength = 128

// deletionPage is GET /integrations/facebook/deletion-status, the page
// Facebook links for a data-deletion request.
var deletionPage = template.Must(template.New("deletion").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>SideQuests · Facebook data deletion</title>
<style>
  body { margin: 0; padding: 48px 16px; background: #F6F3EC; color: #18211C;
         font: 16px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; }
  main { max-width: 520px; margin: 0 auto; background: #fff; border-radius: 18px; padding: 28px; }
  h1 { font-size: 22px; line-height: 1.25; margin: 0 0 12px; }
  p { margin: 0 0 12px; color: #3D4A42; }
  code { background: #EEF2EC; border-radius: 6px; padding: 2px 6px; word-break: break-all; }
</style>
</head>
<body>
<main>
{{if .Found}}
  <h1>Your Facebook data is deleted</h1>
  <p>SideQuests deleted what it imported from Facebook for this account: the connection, the Pages you like, your city and your friends list. Preferences you saved in SideQuests stay until you change them.</p>
  <p>Requested {{.Date}}. Confirmation code <code>{{.Code}}</code></p>
{{else}}
  <h1>We couldn't find that request</h1>
  <p>Check the link Facebook gave you. Confirmation codes stay valid for 90 days.</p>
{{end}}
</main>
</body>
</html>
`))

// DeletionStatus renders the status of a data-deletion request: 200 when
// the code is known (the deletion ran when Facebook called), 404 otherwise.
func (h *H) DeletionStatus(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	data := struct {
		Found      bool
		Code, Date string
	}{Code: code}
	if code != "" && len(code) <= maxCodeLength {
		sess, err := h.d.Store.WebSessions().Peek(r.Context(), code, store.PurposeFacebookDelete)
		switch {
		case err == nil:
			data.Found = true
			data.Date = sess.CreatedAt.UTC().Format("January 2, 2006")
		case !errors.Is(err, store.ErrNotFound):
			log.Error().Err(err).Str("request_id", httpx.RequestID(r)).Msg("facebook: deletion status lookup failed")
			httpx.Error(w, http.StatusInternalServerError, "Something went wrong on our side. Try again.")
			return
		}
	}
	var page bytes.Buffer
	if err := deletionPage.Execute(&page, data); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	status := http.StatusOK
	if !data.Found {
		status = http.StatusNotFound
	}
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(status)
	if _, err := w.Write(page.Bytes()); err != nil {
		log.Warn().Err(err).Msg("write deletion status page")
	}
}
