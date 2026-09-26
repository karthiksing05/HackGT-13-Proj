package api

import (
	"Backend/pkg/api/view"
	"Backend/pkg/contract"
	"Backend/pkg/democlock"
	"Backend/pkg/models"
	"Backend/pkg/store"
	"context"
	"net/http"
	"sync"
	"time"
)

// The demo clock (DEMO_DATE, pkg/democlock): demo accounts live on the demo
// date at the real time of day. Handlers read business time with
// BusinessNow(ctx) for whatever decides or shows what is upcoming, past or
// now, and keep Clock() for tokens, rate limits, web sessions, expiries and
// logs; stores have the same pair (Store.BusinessNow, Store.Now).

// demoCacheTTL is how long whether a user is a demo account is trusted
// (the testable clock decides); demoCacheMax bounds the cache, which starts
// over when full.
const (
	demoCacheTTL = 30 * time.Second
	demoCacheMax = 10000
)

type demoEntry struct {
	demo bool
	at   time.Time
}

// demoState is Deps' demo clock and its per-user cache.
type demoState struct {
	once  sync.Once
	clock *democlock.Clock
	mu    sync.Mutex
	users map[string]demoEntry
}

// DemoClock is DEMO_DATE's clock; nil when it is unset (everyone lives in
// real time and nothing is looked up).
func (d *Deps) DemoClock() *democlock.Clock {
	d.demo.once.Do(func() {
		if d.Cfg != nil {
			d.demo.clock = d.Cfg.DemoClock()
		}
	})
	return d.demo.clock
}

// BusinessNow is now as the request's account lives it: the real time, or
// for a demo account the demo date at the real time of day.
func (d *Deps) BusinessNow(ctx context.Context) time.Time { return democlock.Now(ctx, d.Clock()) }

// ClockFor is u's demo clock: DEMO_DATE's for an account of the demo
// catalog, nil for everyone else.
func (d *Deps) ClockFor(u *models.User) *democlock.Clock {
	if u == nil || u.Catalog != store.CollDemoActivities {
		return nil
	}
	return d.DemoClock()
}

// ForUser is ctx carrying userID's clock: the demo clock for a demo account
// (the catalog is read once per demoCacheTTL), ctx itself otherwise. The
// middleware calls it after bearer auth; work done for a user outside a
// request (the checkout agent) calls it too.
func (d *Deps) ForUser(ctx context.Context, userID string) context.Context {
	c := d.DemoClock()
	if c == nil || userID == "" || !d.isDemo(ctx, userID) {
		return ctx
	}
	return democlock.With(ctx, c)
}

// isDemo reports whether userID plans from the demo catalog. A failed
// lookup (a deleted account, a database hiccup) reads as real time and is
// not cached; the handler meets the same error and reports it.
func (d *Deps) isDemo(ctx context.Context, userID string) bool {
	now := d.Clock()
	d.demo.mu.Lock()
	e, ok := d.demo.users[userID]
	d.demo.mu.Unlock()
	if age := now.Sub(e.at); ok && age >= 0 && age < demoCacheTTL {
		return e.demo
	}
	catalog, err := d.Store.Users().Catalog(ctx, userID)
	if err != nil {
		return false
	}
	demo := catalog == store.CollDemoActivities
	d.demo.mu.Lock()
	if d.demo.users == nil || len(d.demo.users) >= demoCacheMax {
		d.demo.users = map[string]demoEntry{}
	}
	d.demo.users[userID] = demoEntry{demo: demo, at: now}
	d.demo.mu.Unlock()
	return demo
}

// withClock puts the signed-in account's clock on the request (ForUser).
// Without DEMO_DATE it adds nothing, not even a lookup.
func (d *Deps) withClock(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id := UserID(r); id != "" && d.DemoClock() != nil {
			r = r.WithContext(d.ForUser(r.Context(), id))
		}
		next(w, r)
	}
}

// UserView is the signed-in user's own User (GET and PATCH /me, sign-up and
// log-in): view.User at their business time, with demo_date for a demo
// account so the app puts its "today" there too.
func (d *Deps) UserView(u *models.User) contract.User {
	now := d.Clock()
	c := d.ClockFor(u)
	if c == nil {
		return view.User(u, d.Cfg.PublicBaseURL, now)
	}
	out := view.User(u, d.Cfg.PublicBaseURL, c.Shift(now))
	date := c.Date()
	out.DemoDate = &date
	return out
}
