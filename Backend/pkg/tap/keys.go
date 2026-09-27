package tap

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
)

// InMemoryKeyDirectory stores public keys in memory.
type InMemoryKeyDirectory struct {
	mu   sync.RWMutex
	keys map[string]ed25519.PublicKey
}

// NewKeyDirectory creates an empty key directory.
func NewKeyDirectory() *InMemoryKeyDirectory {
	return &InMemoryKeyDirectory{
		keys: make(map[string]ed25519.PublicKey),
	}
}

// Register adds a public key for a key ID.
func (kd *InMemoryKeyDirectory) Register(keyID string, pubKey ed25519.PublicKey) {
	kd.mu.Lock()
	defer kd.mu.Unlock()
	kd.keys[keyID] = pubKey
}

// GetPublicKey implements KeyDirectory.
func (kd *InMemoryKeyDirectory) GetPublicKey(keyID string) (ed25519.PublicKey, error) {
	kd.mu.RLock()
	defer kd.mu.RUnlock()
	key, ok := kd.keys[keyID]
	if !ok {
		return nil, fmt.Errorf("key %q not found", keyID)
	}
	return key, nil
}

// DefaultDemoAgentKeyID is the standard key ID used by the SideQuestz agent.
const DefaultDemoAgentKeyID = "sqz-agent-1"

// Deterministic 32-byte seed for the demo agent key pair:
// This ensures the demo agent and tests always have a working known key pair.
var demoSeed = []byte("sidequestz-demo-tap-agent-seed32")

// DefaultKeyPair returns a deterministic Ed25519 key pair for "sqz-agent-1".
func DefaultKeyPair() (ed25519.PublicKey, ed25519.PrivateKey) {
	reader := deterministicReader{seed: demoSeed, idx: 0}
	pub, priv, err := ed25519.GenerateKey(&reader)
	if err != nil {
		panic(err)
	}
	return pub, priv
}

// NewDemoKeyDirectory is a directory holding only the demo agent key (dev and tests).
func NewDemoKeyDirectory() (*InMemoryKeyDirectory, ed25519.PublicKey, ed25519.PrivateKey) {
	pub, priv := DefaultKeyPair()
	kd := NewKeyDirectory()
	kd.Register(DefaultDemoAgentKeyID, pub)
	return kd, pub, priv
}

// NewDirectory registers the SideQuestz agent's public key (TAP_AGENT_PUBLIC_KEY,
// standard or URL base64 of the 32-byte Ed25519 key) under DefaultDemoAgentKeyID.
// Without one it falls back to the demo key only when allowDemo is set: the
// demo seed is in the repo, so anyone could sign with it.
func NewDirectory(publicKeyB64 string, allowDemo bool) (*InMemoryKeyDirectory, error) {
	if publicKeyB64 == "" {
		if !allowDemo {
			return nil, errors.New("TAP_AGENT_PUBLIC_KEY is required outside dev")
		}
		kd, _, _ := NewDemoKeyDirectory()
		return kd, nil
	}
	pub, err := DecodePublicKey(publicKeyB64)
	if err != nil {
		return nil, err
	}
	kd := NewKeyDirectory()
	kd.Register(DefaultDemoAgentKeyID, pub)
	return kd, nil
}

// PrivateKeyFromSeed reads the agent's signing key: a base64 32-byte Ed25519
// seed (TAP_AGENT_KEY, from sidequestz-admin tap-keygen).
func PrivateKeyFromSeed(seedB64 string) (ed25519.PrivateKey, error) {
	seed, err := base64.StdEncoding.DecodeString(seedB64)
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("TAP agent key must be a base64 32-byte Ed25519 seed")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// DecodePublicKey reads a base64 (standard or URL, padded or not) Ed25519 public key.
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if len(b) != ed25519.PublicKeySize {
				return nil, fmt.Errorf("TAP agent public key is %d bytes, want %d", len(b), ed25519.PublicKeySize)
			}
			return ed25519.PublicKey(b), nil
		}
	}
	return nil, errors.New("TAP agent public key is not base64")
}

type deterministicReader struct {
	seed []byte
	idx  int
}

func (d *deterministicReader) Read(p []byte) (n int, err error) {
	for i := range p {
		p[i] = d.seed[(d.idx+i)%len(d.seed)]
	}
	d.idx = (d.idx + len(p)) % len(d.seed)
	return len(p), nil
}
