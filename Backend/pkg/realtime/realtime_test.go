package realtime_test

import (
	"Backend/pkg/contract"
	"Backend/pkg/realtime"
	"Backend/pkg/testutil"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const wait = 3 * time.Second

func TestWebsocketAuthAndDelivery(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Ava Socket")
	b := srv.Signup(t, "Ben Socket")

	// Header auth, then ?token= fallback; both get the hello.
	connA, _, err := srv.Dial(t, a.Access, false)
	if err != nil {
		t.Fatalf("dial with header: %v", err)
	}
	testutil.ExpectEvent(t, connA, realtime.EventConnected, wait)
	connB, _, err := srv.Dial(t, b.Access, true)
	if err != nil {
		t.Fatalf("dial with ?token=: %v", err)
	}
	if hello := testutil.ExpectEvent(t, connB, realtime.EventConnected, wait); string(hello.Data) != "{}" {
		t.Fatalf("hello data must be {}: %s", hello.Data)
	}

	// Invalid or missing tokens are refused before the upgrade.
	for _, token := range []string{"", "garbage"} {
		if _, res, err := srv.Dial(t, token, false); err == nil || res == nil || res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("token %q must be a 401 handshake: err=%v res=%v", token, err, res)
		}
	}
	if srv.Hub.Connections(a.UserID) != 1 || srv.Hub.Connections(b.UserID) != 1 {
		t.Fatalf("connections: %d %d", srv.Hub.Connections(a.UserID), srv.Hub.Connections(b.UserID))
	}

	// Per-user delivery through the typed helpers and the Deps publisher,
	// followed by a broadcast: A sees both in order, B only the broadcast
	// (a websocket read timeout is permanent, so B is checked by ordering).
	realtime.FriendStatus(srv.Deps.Publish(), []string{a.UserID}, b.UserID, "Free until 8 PM")
	realtime.ForumUpdate(srv.Deps.Publish())
	env := testutil.ExpectEvent(t, connA, realtime.EventFriendStatus, wait)
	var status realtime.FriendStatusData
	if err := json.Unmarshal(env.Data, &status); err != nil || status.UserID != b.UserID || status.StatusLine != "Free until 8 PM" {
		t.Fatalf("friend.status payload: %s %v", env.Data, err)
	}
	if got := srv.Events.Of(realtime.EventFriendStatus); len(got) != 1 || got[0].UserID != a.UserID {
		t.Fatalf("recorder: %+v", got)
	}
	for _, conn := range []*websocket.Conn{connA, connB} {
		_ = conn.SetReadDeadline(time.Now().Add(wait))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != `{"type":"forum.update","data":{}}` {
			t.Fatalf("broadcast envelope (B must not have seen A's event first): %s", raw)
		}
	}

	// A message rendered per recipient.
	msg := contract.Message{ID: "m-1", SenderID: b.UserID, SenderName: "Ben", Text: "hi", SentAt: contract.NewTime(srv.Clock.Now())}
	realtime.MessageNew(srv.Deps.Publish(), a.UserID, "thread-1", msg)
	env = testutil.ExpectEvent(t, connA, realtime.EventMessageNew, wait)
	var payload realtime.MessageNewData
	if err := json.Unmarshal(env.Data, &payload); err != nil || payload.ThreadID != "thread-1" || payload.Message.Text != "hi" || !strings.Contains(string(env.Data), `"sent_at":"`) {
		t.Fatalf("message.new payload: %s %v", env.Data, err)
	}

	// Closing a socket unregisters it.
	_ = connA.Close()
	deadline := time.Now().Add(wait)
	for srv.Hub.Connections(a.UserID) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("closed socket still registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSlowClientIsEvicted(t *testing.T) {
	srv := testutil.New(t)
	c := srv.Signup(t, "Slow Socket")
	conn, _, err := srv.Dial(t, c.Access, false)
	if err != nil {
		t.Fatal(err)
	}
	testutil.ExpectEvent(t, conn, realtime.EventConnected, wait)
	// Never read again: once the kernel buffers and the 64-slot queue are
	// full, the hub must drop the socket instead of blocking.
	big := strings.Repeat("x", 64<<10)
	deadline := time.Now().Add(10 * time.Second)
	for srv.Hub.Connections(c.UserID) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("slow client was never evicted")
		}
		srv.Hub.Send(c.UserID, "transit.delay", map[string]string{"pad": big})
	}
	// The eviction closed the connection on the client side too.
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	// Sending to an evicted user is a no-op, not a panic.
	srv.Hub.Send(c.UserID, "forum.update", nil)
}

func TestMaxSocketsPerUser(t *testing.T) {
	srv := testutil.New(t)
	u := srv.Signup(t, "Many Devices")
	var conns []*websocket.Conn
	for i := 0; i < 6; i++ {
		conn, _, err := srv.Dial(t, u.Access, i%2 == 0)
		if err != nil {
			t.Fatal(err)
		}
		testutil.ExpectEvent(t, conn, realtime.EventConnected, wait)
		conns = append(conns, conn)
	}
	deadline := time.Now().Add(wait)
	for srv.Hub.Connections(u.UserID) != 5 {
		if time.Now().After(deadline) {
			t.Fatalf("connections = %d, want 5", srv.Hub.Connections(u.UserID))
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The oldest socket was closed; the newest still receives.
	if _, err := testutil.ReadEvent(t, conns[0], wait); err == nil {
		t.Fatal("oldest socket still open")
	}
	srv.Hub.Send(u.UserID, "thread.read", realtime.ThreadReadData{ThreadID: "t1"})
	testutil.ExpectEvent(t, conns[5], realtime.EventThreadRead, wait)
}

func TestRecorderAndNoop(t *testing.T) {
	rec := realtime.NewRecorder(nil)
	rec.Send("u1", "a", 1)
	rec.SendMany([]string{"u1", "u2"}, "b", nil)
	rec.Broadcast("c", nil)
	if len(rec.Events()) != 4 || len(rec.For("u1")) != 3 || len(rec.For("u2")) != 2 || len(rec.Of("b")) != 2 {
		t.Fatalf("recorder counts: %+v", rec.Events())
	}
	rec.Reset()
	if len(rec.Events()) != 0 {
		t.Fatal("Reset did not clear")
	}
	realtime.Noop{}.Send("u", "x", nil)
	realtime.Noop{}.Broadcast("x", nil)
}
