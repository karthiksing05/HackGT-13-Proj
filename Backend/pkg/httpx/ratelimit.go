package httpx

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TooManyRequests is the 429 sentence.
const TooManyRequests = "Too many attempts. Try again in a minute."

// Limiter is a per-key token bucket: perMinute tokens refill continuously up
// to burst. Idle keys are swept so the map stays small.
type Limiter struct {
	mu        sync.Mutex
	rate      float64 // tokens per second
	burst     float64
	buckets   map[string]*bucket
	now       func() time.Time
	lastSweep time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

const (
	sweepEvery = 5 * time.Minute
	idleFor    = 10 * time.Minute
)

// NewLimiter allows perMinute requests per key with the given burst.
func NewLimiter(perMinute, burst int) *Limiter {
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		rate:    float64(perMinute) / 60,
		burst:   float64(burst),
		buckets: map[string]*bucket{},
		now:     time.Now,
	}
}

// SetClock replaces the clock (tests).
func (l *Limiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.now = now
}

// Allow takes one token for key, reporting whether one was available.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) > sweepEvery {
		for k, b := range l.buckets {
			if now.Sub(b.last) > idleFor {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Middleware answers 429 when keyFn(r)'s bucket is empty; an empty key is never limited.
func (l *Limiter) Middleware(keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if key := keyFn(r); key != "" && !l.Allow(key) {
				Error(w, http.StatusTooManyRequests, TooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP is the caller's address: the first X-Forwarded-For hop (or
// X-Real-IP) behind a trusted proxy, else RemoteAddr without the port.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
