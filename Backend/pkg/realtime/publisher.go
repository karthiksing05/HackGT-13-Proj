// Package realtime is the WS /ws hub and the Publisher every handler emits
// through. Envelopes are exactly {"type": "<event>", "data": {…}}.
package realtime

import "sync"

// Publisher fans events out to connected devices. Payloads whose shape
// depends on the viewer (sender_name "You", is_host, unread) are rendered per
// recipient and sent one at a time.
type Publisher interface {
	Send(userID, typ string, data any)
	SendMany(userIDs []string, typ string, data any)
	Broadcast(typ string, data any)
}

// Noop drops everything (tools, tests without realtime).
type Noop struct{}

func (Noop) Send(string, string, any)       {}
func (Noop) SendMany([]string, string, any) {}
func (Noop) Broadcast(string, any)          {}

// Event is one recorded emission.
type Event struct {
	UserID    string // "" for broadcasts
	Broadcast bool
	Type      string
	Data      any
}

// Recorder keeps every event for assertions and forwards to Next when set
// (a real Hub in websocket tests).
type Recorder struct {
	mu     sync.Mutex
	events []Event
	Next   Publisher
}

// NewRecorder builds a recorder forwarding to next (nil = record only).
func NewRecorder(next Publisher) *Recorder { return &Recorder{Next: next} }

func (r *Recorder) record(e Event) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
}

func (r *Recorder) Send(userID, typ string, data any) {
	r.record(Event{UserID: userID, Type: typ, Data: data})
	if r.Next != nil {
		r.Next.Send(userID, typ, data)
	}
}

func (r *Recorder) SendMany(userIDs []string, typ string, data any) {
	for _, id := range userIDs {
		r.record(Event{UserID: id, Type: typ, Data: data})
	}
	if r.Next != nil {
		r.Next.SendMany(userIDs, typ, data)
	}
}

func (r *Recorder) Broadcast(typ string, data any) {
	r.record(Event{Broadcast: true, Type: typ, Data: data})
	if r.Next != nil {
		r.Next.Broadcast(typ, data)
	}
}

// Events is a copy of everything recorded so far.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

// For is every event addressed to a user (broadcasts included).
func (r *Recorder) For(userID string) []Event {
	var out []Event
	for _, e := range r.Events() {
		if e.Broadcast || e.UserID == userID {
			out = append(out, e)
		}
	}
	return out
}

// Of is every event of one type.
func (r *Recorder) Of(typ string) []Event {
	var out []Event
	for _, e := range r.Events() {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// Reset forgets the recorded events.
func (r *Recorder) Reset() {
	r.mu.Lock()
	r.events = nil
	r.mu.Unlock()
}
