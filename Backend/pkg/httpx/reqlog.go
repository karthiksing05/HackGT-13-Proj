package httpx

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

type requestIDKey struct{}

// RequestID returns the id assigned by RequestLog ("" outside the middleware).
func RequestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

// WithRequestID stores an id on a context (tests).
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// statusWriter records the status and size for the log line.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer (hijack for websockets).
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// RequestLog assigns an X-Request-ID (honouring an incoming one) and logs one
// line per request with method, path, status, duration and size.
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(sw, r.WithContext(WithRequestID(r.Context(), id)))
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		ev := log.Info()
		if status >= 500 {
			ev = log.Error()
		} else if status >= 400 {
			ev = log.Warn()
		}
		ev.Str("request_id", id).Str("method", r.Method).Str("path", r.URL.Path).
			Int("status", status).Int("bytes", sw.bytes).Dur("duration", time.Since(start)).Msg("http")
	})
}
