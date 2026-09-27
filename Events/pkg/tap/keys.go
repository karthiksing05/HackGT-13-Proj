package tap

import (
	"crypto/ed25519"
	"encoding/base64"
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

// NewDefaultKeyDirectory creates a key directory pre-populated with the default agent key.
func NewDefaultKeyDirectory(customSeedB64 string) (*InMemoryKeyDirectory, ed25519.PublicKey, ed25519.PrivateKey) {
	kd := NewKeyDirectory()
	var pub ed25519.PublicKey
	var priv ed25519.PrivateKey

	if customSeedB64 != "" {
		seedBytes, err := base64.StdEncoding.DecodeString(customSeedB64)
		if err == nil && len(seedBytes) >= 32 {
			priv = ed25519.NewKeyFromSeed(seedBytes[:32])
			pub = priv.Public().(ed25519.PublicKey)
		} else {
			pub, priv = DefaultKeyPair()
		}
	} else {
		pub, priv = DefaultKeyPair()
	}

	kd.Register(DefaultDemoAgentKeyID, pub)
	return kd, pub, priv
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
