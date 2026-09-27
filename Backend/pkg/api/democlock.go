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

// The demo clock (DEMO_DATE, pkg/democlock): while the demo collection is
// selected, signed-in accounts live on the demo date at the real time of day.
// Handlers read business time with
// BusinessNow(ctx) for whatever decides or shows what is upcoming, past or
// now, and keep Clock() for tokens, rate limits, web sessions, expiries and
// logs; stores have the same pair (Store.BusinessNow, Store.Now).

// demoState holds the optional demo clock.
type demoState struct {
	once  sync.Once
	clock *democlock.Clock
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

// BusinessNow is the request's business time, using the demo date only
// while the demo collection is selected.
func (d *Deps) BusinessNow(ctx context.Context) time.Time { return democlock.Now(ctx, d.Clock()) }

// ClockFor returns DEMO_DATE's clock while the demo collection is selected.
func (d *Deps) ClockFor(u *models.User) *democlock.Clock {
	if u == nil || store.ActivityCollection != store.CollDemoActivities {
		return nil
	}
	return d.DemoClock()
}

// ForUser applies the selected collection's clock to signed-in requests
// and background work. It does not load the user.
func (d *Deps) ForUser(ctx context.Context, userID string) context.Context {
	if userID == "" || store.ActivityCollection != store.CollDemoActivities {
		return ctx
	}
	return democlock.With(ctx, d.DemoClock())
}

// withClock puts the signed-in account's clock on the request (ForUser).
// No account lookup is needed.
func (d *Deps) withClock(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if id := UserID(r); id != "" && store.ActivityCollection == store.CollDemoActivities {
			r = r.WithContext(d.ForUser(r.Context(), id))
		}
		next(w, r)
	}
}

// UserView is the signed-in user's own User (GET and PATCH /me, sign-up and
// log-in): view.User at their business time, with demo_date while the demo
// collection is selected so the app puts its "today" there too.
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
