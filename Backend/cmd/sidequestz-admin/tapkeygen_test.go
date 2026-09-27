package main

import (
	"Backend/pkg/tap"
	"bytes"
	"crypto/ed25519"
	"regexp"
	"testing"
)

func TestTapKeygenPrintsAMatchingPair(t *testing.T) {
	var out bytes.Buffer
	if code := tapKeygen(&out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	seed := regexp.MustCompile(`TAP_AGENT_KEY=(\S+)`).FindStringSubmatch(out.String())
	pub := regexp.MustCompile(`TAP_AGENT_PUBLIC_KEY=(\S+)`).FindStringSubmatch(out.String())
	if seed == nil || pub == nil {
		t.Fatalf("output: %s", out.String())
	}
	priv, err := tap.PrivateKeyFromSeed(seed[1])
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := tap.DecodePublicKey(pub[1])
	if err != nil || !bytes.Equal(decoded, priv.Public().(ed25519.PublicKey)) {
		t.Fatalf("public key does not match the seed: %v", err)
	}
}
