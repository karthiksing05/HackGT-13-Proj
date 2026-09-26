// Package democlock is the per-account demo clock. With DEMO_DATE set, the
// accounts of the demo catalog (Sandy Byte and her bots) live on that date:
// their business time is the real instant moved by whole calendar days in
// DEMO_TZ so that its local date is DEMO_DATE, keeping the local time of
// day. It is recomputed on every call, so it stays on that date whatever
// the real day is.
//
// Two clocks, never mixed: business time decides and shows what is
// upcoming, past or now (plans, the Forum, chats, calendar days); real time
// runs everything security- or expiry-related (tokens, rate limits, reset
// codes, web sessions, logs and every TTL expiresAt, which MongoDB compares
// with the wall clock). A request carries its account's clock in its
// context (With); code without one lives in real time.
package democlock

import (
	"context"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // the VPS may not ship zoneinfo
)

// Layout is DEMO_DATE's format.
const Layout = "2006-01-02"

// DefaultTZ is DEMO_TZ's default (pkg/config).
const DefaultTZ = "America/New_York"

// Clock pins demo accounts to one calendar date in one zone.
type Clock struct {
	year  int
	month time.Month
	day   int
	loc   *time.Location
}

// New parses DEMO_DATE (YYYY-MM-DD) as a date in the zone tz (DEMO_TZ; ""
// is the default zone).
func New(date, tz string) (*Clock, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		tz = DefaultTZ
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("DEMO_TZ %q: %w", tz, err)
	}
	day, err := time.ParseInLocation(Layout, strings.TrimSpace(date), loc)
	if err != nil {
		return nil, fmt.Errorf("DEMO_DATE must be a date like 2026-09-24, got %q", date)
	}
	y, m, d := day.Date()
	return &Clock{year: y, month: m, day: d, loc: loc}, nil
}

// Date is the demo date, "2006-01-02".
func (c *Clock) Date() string {
	return time.Date(c.year, c.month, c.day, 0, 0, 0, 0, c.loc).Format(Layout)
}

// Location is DEMO_TZ.
func (c *Clock) Location() *time.Location { return c.loc }

// Shift is the business instant of a real one: the same local time of day
// in DEMO_TZ on the demo date (calendar arithmetic, so a DST change between
// the two days keeps the wall clock; a wall time that does not exist on the
// demo date normalizes as time.Date does). The result is in real's location.
func (c *Clock) Shift(real time.Time) time.Time {
	l := real.In(c.loc)
	return time.Date(c.year, c.month, c.day, l.Hour(), l.Minute(), l.Second(), l.Nanosecond(), c.loc).In(real.Location())
}

type ctxKey struct{}

// With marks ctx as a demo account's: its business time follows c. A nil
// clock leaves ctx as it is.
func With(ctx context.Context, c *Clock) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, c)
}

// From is the demo clock ctx carries; nil for everyone else.
func From(ctx context.Context) *Clock {
	c, _ := ctx.Value(ctxKey{}).(*Clock)
	return c
}

// Now is business time at the real instant real: real itself unless ctx
// carries a demo clock.
func Now(ctx context.Context, real time.Time) time.Time {
	if c := From(ctx); c != nil {
		return c.Shift(real)
	}
	return real
}

// RealAt is the real instant that is the business instant t for ctx's
// account, at the offset the clock has at realNow: a TTL expiry for a
// business deadline (a free-now post's until) expires at the same moment.
func RealAt(ctx context.Context, t, realNow time.Time) time.Time {
	return t.Add(realNow.Sub(Now(ctx, realNow)))
}
