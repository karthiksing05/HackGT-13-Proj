// Package facebook is the Facebook connector (backend-contract §4 E): the
// Login dialog and its callback, the Graph API import that turns liked Pages
// into suggested ratings and interests, disconnect, and Facebook's
// deauthorize and data-deletion callbacks. The app never sees the app secret
// or the user token; the token is stored sealed (crypto.go) and every Graph
// call carries appsecret_proof (graph.go). The router registers this package
// before integrations, so /integrations/facebook… is matched here.
package facebook

import (
	"Backend/pkg/api"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// H holds the handlers. client is the real Graph API client, nil without
// FB_APP_ID / FB_APP_SECRET; box is nil when no token key can be derived.
type H struct {
	d      *api.Deps
	client Graph
	box    *tokenBox
}

// Register mounts the §4 E routes.
func Register(r *mux.Router, d *api.Deps) {
	h := newH(d)
	r.Handle("/integrations/facebook", d.Protect(h.Connection)).Methods("GET")
	r.Handle("/integrations/facebook", d.Protect(h.Disconnect)).Methods("DELETE")
	r.Handle("/integrations/facebook/connect", d.Protect(h.Connect)).Methods("POST")
	r.HandleFunc("/integrations/facebook/callback", h.Callback).Methods("GET")
	r.Handle("/integrations/facebook/import", d.Protect(h.Import)).Methods("POST")
	r.HandleFunc("/integrations/facebook/deauthorize", h.Deauthorize).Methods("POST")
	r.HandleFunc("/integrations/facebook/data-deletion", h.DataDeletion).Methods("POST")
	r.HandleFunc("/integrations/facebook/deletion-status", h.DeletionStatus).Methods("GET")
}

func newH(d *api.Deps) *H {
	h := &H{d: d}
	if c := NewClient(d.Cfg.FBAppID, d.Cfg.FBAppSecret, graphVersion(d.Cfg.FBGraphVersion)); c != nil {
		h.client = c
		if !strings.HasPrefix(d.Cfg.PublicBaseURL, "https://") {
			log.Warn().Str("redirect_uri", h.redirectURI()).
				Msg("Facebook Login needs an https redirect URI; plain http works only for localhost while the Meta app is in Development mode")
		}
	} else {
		log.Warn().Msg("Facebook connector off: FB_APP_ID and FB_APP_SECRET are not set")
	}
	box, err := newTokenBox(d.Cfg.FBTokenKey, d.Cfg.JWTSecret)
	if err != nil {
		log.Error().Err(err).Msg("Facebook tokens cannot be stored; the connector answers 503")
	}
	h.box = box
	return h
}

// graphVersion is FB_GRAPH_VERSION as a path segment ("v26.0").
func graphVersion(v string) string {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return "v26.0"
	case !strings.HasPrefix(v, "v"):
		return "v" + v
	}
	return v
}

// injected holds Graph replacements per Deps (UseGraph).
var injected sync.Map

// UseGraph points the Facebook handlers of d at g instead of the real Graph
// API; tests pass a FakeGraph so nothing reaches Facebook. Call it before
// serving requests.
func UseGraph(d *api.Deps, g Graph) { injected.Store(d, g) }

// graph is the Graph to call, nil when the connector is off.
func (h *H) graph() Graph {
	if g, ok := injected.Load(h.d); ok {
		return g.(Graph)
	}
	return h.client
}

// ready reports whether the connector can run a Login or an import: an app
// id and secret, a Graph client and a token key.
func (h *H) ready() bool {
	return h.d.Cfg.FBAppID != "" && h.d.Cfg.FBAppSecret != "" && h.graph() != nil && h.box != nil
}
