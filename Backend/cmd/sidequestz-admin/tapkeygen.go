package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

// tapKeygen prints a fresh Ed25519 key pair for the checkout agent's signed
// requests: the seed goes in Backend's TAP_AGENT_KEY (secret), the public key
// in Events' TAP_AGENT_PUBLIC_KEY.
func tapKeygen(w io.Writer) int {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tap-keygen: %v\n", err)
		return 1
	}
	fmt.Fprintf(w, "# Backend (/opt/backend/.env), keep secret:\nTAP_AGENT_KEY=%s\n\n", base64.StdEncoding.EncodeToString(priv.Seed()))
	fmt.Fprintf(w, "# Events (/opt/events/.env):\nTAP_AGENT_PUBLIC_KEY=%s\n", base64.StdEncoding.EncodeToString(pub))
	return 0
}
