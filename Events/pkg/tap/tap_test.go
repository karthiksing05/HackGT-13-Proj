package tap

import (
	"errors"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type memoryNonceStore struct {
	mu     sync.Mutex
	nonces map[string]time.Time
}

func newMemoryNonceStore() *memoryNonceStore {
	return &memoryNonceStore{nonces: make(map[string]time.Time)}
}

func (m *memoryNonceStore) CheckAndRecordNonce(nonce string, expiresAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.nonces[nonce]; exists {
		return errors.New("nonce already used")
	}
	m.nonces[nonce] = expiresAt
	return nil
}

func TestTAPSignAndVerifyRoundTrip(t *testing.T) {
	kd, _, priv := NewDemoKeyDirectory()
	nonces := newMemoryNonceStore()

	req := httptest.NewRequest("GET", "/api/events/sunset-jazz/offer", nil)
	req.Host = "events.sidequestz.tech"
	now := time.Now().UTC()

	err := Sign(req, priv, DefaultDemoAgentKeyID, "agent-browser-auth", "events.sidequestz.tech", now)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	parsed, err := Verify(req, kd, nonces, "agent-browser-auth", "events.sidequestz.tech", now)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}
	if parsed.KeyID != DefaultDemoAgentKeyID {
		t.Errorf("expected KeyID %s, got %s", DefaultDemoAgentKeyID, parsed.KeyID)
	}
	if parsed.Tag != "agent-browser-auth" {
		t.Errorf("expected Tag agent-browser-auth, got %s", parsed.Tag)
	}
}

func TestTAPReplayedNonceRejected(t *testing.T) {
	kd, _, priv := NewDemoKeyDirectory()
	nonces := newMemoryNonceStore()

	req := httptest.NewRequest("POST", "/api/orders", nil)
	req.Host = "events.sidequestz.tech"
	now := time.Now().UTC()

	if err := Sign(req, priv, DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", now); err != nil {
		t.Fatal(err)
	}

	// First verify should succeed
	if _, err := Verify(req, kd, nonces, "agent-payer-auth", "events.sidequestz.tech", now); err != nil {
		t.Fatalf("first verify failed: %v", err)
	}

	// Replayed request must be rejected with ErrReplayedNonce
	_, err := Verify(req, kd, nonces, "agent-payer-auth", "events.sidequestz.tech", now)
	if !errors.Is(err, ErrReplayedNonce) {
		t.Fatalf("expected ErrReplayedNonce on replay, got: %v", err)
	}
}

func TestTAPExpiredRejected(t *testing.T) {
	kd, _, priv := NewDemoKeyDirectory()
	nonces := newMemoryNonceStore()

	req := httptest.NewRequest("GET", "/api/orders/SL-12345", nil)
	req.Host = "events.sidequestz.tech"
	now := time.Now().UTC()

	if err := Sign(req, priv, DefaultDemoAgentKeyID, "agent-browser-auth", "events.sidequestz.tech", now); err != nil {
		t.Fatal(err)
	}

	// Verify 10 minutes in the future (expires is +5 minutes)
	future := now.Add(10 * time.Minute)
	_, err := Verify(req, kd, nonces, "agent-browser-auth", "events.sidequestz.tech", future)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got: %v", err)
	}
}

func TestTAPWrongTagRejected(t *testing.T) {
	kd, _, priv := NewDemoKeyDirectory()
	nonces := newMemoryNonceStore()

	req := httptest.NewRequest("POST", "/api/orders", nil)
	req.Host = "events.sidequestz.tech"
	now := time.Now().UTC()

	// Signed with browser auth instead of payer auth
	if err := Sign(req, priv, DefaultDemoAgentKeyID, "agent-browser-auth", "events.sidequestz.tech", now); err != nil {
		t.Fatal(err)
	}

	_, err := Verify(req, kd, nonces, "agent-payer-auth", "events.sidequestz.tech", now)
	if !errors.Is(err, ErrWrongTag) {
		t.Fatalf("expected ErrWrongTag, got: %v", err)
	}
}

func TestTAPWrongAuthorityRejected(t *testing.T) {
	kd, _, priv := NewDemoKeyDirectory()
	nonces := newMemoryNonceStore()

	req := httptest.NewRequest("GET", "/api/events/sunset-jazz/offer", nil)
	req.Host = "evil.example.com"
	now := time.Now().UTC()

	if err := Sign(req, priv, DefaultDemoAgentKeyID, "agent-browser-auth", "evil.example.com", now); err != nil {
		t.Fatal(err)
	}

	_, err := Verify(req, kd, nonces, "agent-browser-auth", "events.sidequestz.tech", now)
	if !errors.Is(err, ErrWrongAuthority) {
		t.Fatalf("expected ErrWrongAuthority, got: %v", err)
	}
}
