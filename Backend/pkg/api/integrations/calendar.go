package integrations

import (
	"Backend/pkg/api"
	"Backend/pkg/contract"
	"Backend/pkg/httpx"
	"Backend/pkg/store"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/mux"
)

// providerNames are the calendars as the app names them.
var providerNames = map[string]string{
	string(contract.ProviderGoogle):  "Google Calendar",
	string(contract.ProviderOutlook): "Outlook Calendar",
}

// List is GET /integrations → the two calendars, google then outlook.
func (h *H) List(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, []contract.Integration{
		{Provider: contract.ProviderGoogle, Connected: user.Integrations.Google.Connected},
		{Provider: contract.ProviderOutlook, Connected: user.Integrations.Outlook.Connected},
	})
}

// Connect is POST /integrations/{provider}/connect → {url} of the hosted
// start page, a one-time link for the caller that works for pageTTL.
func (h *H) Connect(w http.ResponseWriter, r *http.Request) {
	provider := mux.Vars(r)["provider"]
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	token, err := h.d.Store.WebSessions().Create(r.Context(), user.ID.Hex(), store.PurposeCalendarConnect, provider, pageTTL)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, contract.URLResponse{URL: h.publicURL("/integrations/" + provider + "/start?t=" + url.QueryEscape(token))})
}

// Start is GET /integrations/{provider}/start?t= (public: the link is the
// credential). Simulated: it consumes the link, marks the calendar connected
// for the link's account without contacting the provider, and sends the
// browser back to the app with a 302; the page says that no calendar data
// is read. A used, expired or unknown link gets a page saying so.
func (h *H) Start(w http.ResponseWriter, r *http.Request) {
	provider := mux.Vars(r)["provider"]
	token := r.URL.Query().Get("t")
	ctx := r.Context()
	sessions := h.d.Store.WebSessions()
	sess, err := sessions.Peek(ctx, token, store.PurposeCalendarConnect)
	if err == nil && sess.Provider != provider {
		err = store.ErrNotFound // a link made for the other calendar
	}
	if err == nil {
		sess, err = sessions.Consume(ctx, token, store.PurposeCalendarConnect)
	}
	if err == nil {
		err = h.d.Store.Users().SetIntegration(ctx, sess.UserID, provider, true)
	}
	if err != nil {
		pageFail(w, r, err)
		return
	}
	name := providerNames[provider]
	redirect(w, "sidequestz://integrations/"+provider+"/done?status=connected", page{
		Title: name + " connected",
		Body: []string{
			"Calendar sync is simulated in this version of SideQuests: " + name + " is marked as connected without signing in to it, and SideQuests doesn't read any calendar data.",
			"Taking you back to SideQuests…",
		},
	})
}

// Disconnect is DELETE /integrations/{provider} → 204.
func (h *H) Disconnect(w http.ResponseWriter, r *http.Request) {
	user, err := h.d.CurrentUser(r)
	if err != nil {
		api.Fail(w, r, err)
		return
	}
	if err := h.d.Store.Users().SetIntegration(r.Context(), user.ID.Hex(), mux.Vars(r)["provider"], false); err != nil {
		api.Fail(w, r, err)
		return
	}
	httpx.NoContent(w)
}

// publicURL is an absolute URL on this server (PUBLIC_BASE_URL + path).
func (h *H) publicURL(path string) string {
	return strings.TrimRight(h.d.Cfg.PublicBaseURL, "/") + path
}
