package testutil

import (
	"Backend/pkg/realtime"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// EventCount counts recorded events of typ for userID (broadcasts included).
func (s *Server) EventCount(userID, typ string) int {
	n := 0
	for _, e := range s.Events.For(userID) {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// EventData decodes an event's data into v through JSON (as the app sees it).
func EventData(t testing.TB, e realtime.Event, v any) {
	t.Helper()
	raw, err := json.Marshal(e.Data)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("decode event data %s: %v", raw, err)
	}
}

// Dial opens a websocket to /ws with the token in the Authorization header
// (viaQuery=false) or ?token= (viaQuery=true). It returns the handshake
// response too, so a refused connection can be asserted.
func (s *Server) Dial(t testing.TB, token string, viaQuery bool) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := s.WSURL()
	header := http.Header{}
	if viaQuery {
		url += "?token=" + token
	} else if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	conn, res, err := websocket.DefaultDialer.Dial(url, header)
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn, res, err
}

// WSEnvelope is one received {"type", "data"} message.
type WSEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ReadEvent reads the next message within timeout.
func ReadEvent(t testing.TB, conn *websocket.Conn, timeout time.Duration) (WSEnvelope, error) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	var env WSEnvelope
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("bad envelope %s: %v", raw, err)
	}
	return env, nil
}

// ExpectEvent reads the next message and asserts its type.
func ExpectEvent(t testing.TB, conn *websocket.Conn, typ string, timeout time.Duration) WSEnvelope {
	t.Helper()
	env, err := ReadEvent(t, conn, timeout)
	if err != nil {
		t.Fatalf("waiting for %s: %v", typ, err)
	}
	if env.Type != typ {
		t.Fatalf("expected %s, got %s (%s)", typ, env.Type, env.Data)
	}
	return env
}
