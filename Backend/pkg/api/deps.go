// Package api holds what every handler package shares: Deps, the planner
// seam, the store-error → status mapping and the 501 stub helper. Area
// packages (pkg/api/auth, me, …) expose Register(r *mux.Router, d *api.Deps)
// and keep their handlers as methods on a local H struct{ d *api.Deps }.
package api

import (
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/middleware"
	"Backend/pkg/ml"
	"Backend/pkg/models"
	"Backend/pkg/realtime"
	"Backend/pkg/store"
	"context"
	"net/http"
	"sync"
	"time"
)

// StopDetail is what the planner knows about a generated stop when an
// itinerary is saved (backend-contract §4 F).
type StopDetail struct {
	ActivityID  string
	PriceCents  *int
	WebsiteURL  *string
	Bookable    bool
	DurationMin int
}

// Planner is the seam pkg/api/planning calls; pkg/planner implements it and
// main wires it. A nil Planner answers 503 "Planning is warming up".
type Planner interface {
	Generate(ctx context.Context, user *models.User, req contract.PlanRequest, tz *time.Location) (contract.PlanBatch, error)
	More(ctx context.Context, user *models.User, cursor string) (contract.PlanBatch, error)
	Route(ctx context.Context, user *models.User, req contract.RouteRequest) (contract.RouteResult, error)
	Alternatives(ctx context.Context, user *models.User, req contract.AlternativesRequest) ([]contract.PlanAlternative, error)
	ResolveStop(ctx context.Context, stopID string) (*StopDetail, error)
}

// Deps is everything a handler needs. Use it by pointer only.
type Deps struct {
	Store   *store.Store
	Hub     realtime.Publisher // Send / SendMany / Broadcast; nil = Noop
	Cfg     *config.Config
	Now     func() time.Time // testable clock; nil = time.Now
	Planner Planner
	// Profiles refreshes users' taste vectors (see profiles.go); nil = no-op.
	Profiles Profiles
	// ML is the raw ML client for pkg/profiles and the planner; handlers use
	// Profiles instead.
	ML *ml.Client

	authOnce sync.Once
	auth     *middleware.Auth
}

// Clock is the current time from the testable clock.
func (d *Deps) Clock() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

// Publish is the realtime publisher, never nil.
func (d *Deps) Publish() realtime.Publisher {
	if d.Hub == nil {
		return realtime.Noop{}
	}
	return d.Hub
}

// Auth is the shared access-token verifier.
func (d *Deps) Auth() *middleware.Auth {
	d.authOnce.Do(func() {
		d.auth = middleware.NewAuth(d.Cfg.JWTSecret, d.Clock)
	})
	return d.auth
}

// Protect requires a bearer access token (401 otherwise).
func (d *Deps) Protect(h http.HandlerFunc) http.Handler { return d.Auth().Bearer(h) }

// Optional attaches the user when a valid token is present and continues anonymously otherwise.
func (d *Deps) Optional(h http.HandlerFunc) http.Handler { return d.Auth().Optional(h) }

// UserID is the authenticated user's id ("" on public routes without a token).
func UserID(r *http.Request) string { return middleware.UserID(r) }

// CurrentUser loads the authenticated user's document; a token whose user
// no longer exists is a 401 (the app then signs out).
func (d *Deps) CurrentUser(r *http.Request) (*models.User, error) {
	id := UserID(r)
	if id == "" {
		return nil, unauthorized()
	}
	user, err := d.Store.Users().ByID(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			return nil, unauthorized()
		}
		return nil, err
	}
	return user, nil
}
