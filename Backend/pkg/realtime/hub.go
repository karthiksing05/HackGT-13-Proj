package realtime

import (
	"Backend/pkg/httpx"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	sendBuffer = 64 // messages queued per socket; a full queue evicts the socket
	maxPerUser = 5  // oldest socket is closed when a sixth connects
	pingPeriod = 25 * time.Second
	readWait   = 60 * time.Second
	writeWait  = 10 * time.Second
	readLimit  = 4 << 10
)

// Hub owns every websocket and implements Publisher. Auth: Authorization:
// Bearer first, ?token= as the fallback; an invalid token is a 401 before
// the upgrade. On connect the socket receives {"type":"connected","data":{}}.
type Hub struct {
	auth     func(token string) (userID string, ok bool)
	upgrader websocket.Upgrader

	mu      sync.Mutex
	clients map[string]map[*client]struct{}
	seq     uint64
	closed  bool
}

// NewHub builds a hub; auth validates an access token and returns its user.
func NewHub(auth func(token string) (string, bool)) *Hub {
	return &Hub{
		auth:    auth,
		clients: map[string]map[*client]struct{}{},
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			CheckOrigin:     func(*http.Request) bool { return true }, // no cookies: origin carries no credentials
		},
	}
}

type envelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// marshal builds the wire envelope; nil data is {}.
func marshal(typ string, data any) ([]byte, bool) {
	if data == nil {
		data = struct{}{}
	}
	b, err := json.Marshal(envelope{Type: typ, Data: data})
	if err != nil {
		log.Error().Err(err).Str("type", typ).Msg("realtime: marshal")
		return nil, false
	}
	return b, true
}

// ServeHTTP is GET /ws.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := ""
	if header := r.Header.Get("Authorization"); header != "" {
		if scheme, rest, ok := strings.Cut(header, " "); ok && strings.EqualFold(scheme, "Bearer") {
			token = strings.TrimSpace(rest)
		}
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	userID, ok := h.auth(token)
	if token == "" || !ok {
		httpx.Error(w, http.StatusUnauthorized, "Your session expired. Sign in again.")
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote the response
	}
	c := &client{hub: h, conn: conn, userID: userID, send: make(chan []byte, sendBuffer), done: make(chan struct{})}
	if !h.register(c) {
		_ = conn.Close()
		return
	}
	if hello, ok := marshal("connected", nil); ok {
		c.send <- hello
	}
	go c.writePump()
	go c.readPump()
}

// register adds a socket, evicting the user's oldest one past the limit.
func (h *Hub) register(c *client) bool {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return false
	}
	h.seq++
	c.seq = h.seq
	set, ok := h.clients[c.userID]
	if !ok {
		set = map[*client]struct{}{}
		h.clients[c.userID] = set
	}
	var oldest *client
	if len(set) >= maxPerUser {
		for other := range set {
			if oldest == nil || other.seq < oldest.seq {
				oldest = other
			}
		}
	}
	set[c] = struct{}{}
	h.mu.Unlock()
	if oldest != nil {
		oldest.close()
	}
	return true
}

func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set, ok := h.clients[c.userID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clients, c.userID)
		}
	}
}

// targets snapshots the sockets to deliver to (nil userIDs = everyone).
func (h *Hub) targets(userIDs []string) []*client {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*client
	if userIDs == nil {
		for _, set := range h.clients {
			for c := range set {
				out = append(out, c)
			}
		}
		return out
	}
	for _, id := range userIDs {
		for c := range h.clients[id] {
			out = append(out, c)
		}
	}
	return out
}

// deliver queues a message; a socket that cannot keep up is closed.
func deliver(c *client, msg []byte) {
	select {
	case c.send <- msg:
	default:
		log.Warn().Str("user_id", c.userID).Msg("realtime: slow client evicted")
		c.close()
	}
}

func (h *Hub) Send(userID, typ string, data any) {
	h.SendMany([]string{userID}, typ, data)
}

func (h *Hub) SendMany(userIDs []string, typ string, data any) {
	if len(userIDs) == 0 {
		return
	}
	msg, ok := marshal(typ, data)
	if !ok {
		return
	}
	for _, c := range h.targets(userIDs) {
		deliver(c, msg)
	}
}

func (h *Hub) Broadcast(typ string, data any) {
	msg, ok := marshal(typ, data)
	if !ok {
		return
	}
	for _, c := range h.targets(nil) {
		deliver(c, msg)
	}
}

// Connections is the number of open sockets for a user (tests, metrics).
func (h *Hub) Connections(userID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients[userID])
}

// Close refuses new sockets and closes every open one (shutdown).
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	for _, c := range h.targets(nil) {
		c.close()
	}
}

// client is one socket. send is never closed by a sender: close() signals
// done and closes the connection, exactly once.
type client struct {
	hub    *Hub
	conn   *websocket.Conn
	userID string
	seq    uint64
	send   chan []byte
	done   chan struct{}
	once   sync.Once
}

func (c *client) close() {
	c.once.Do(func() {
		c.hub.unregister(c)
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()
	for {
		select {
		case msg := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// readPump drains the socket (the app never sends) to run the pong handler
// and notice a closed connection.
func (c *client) readPump() {
	defer c.close()
	c.conn.SetReadLimit(readLimit)
	_ = c.conn.SetReadDeadline(time.Now().Add(readWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(readWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(readWait))
	}
}
