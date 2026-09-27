package tap

import (
	"net/http/httptest"
	"testing"
)

// The same vector is in Events/pkg/tap/vector_test.go: the agent (Backend)
// and the merchant (Events) keep separate copies of this package, and both
// must produce this signature for this request.
const (
	vectorSignatureInput = `sig2=("@authority" "@path");created=1790000000;expires=1790000300;keyid="sqz-agent-1";alg="ed25519";nonce="nonce_vector";tag="agent-payer-auth"`
	vectorSignature      = "sig2=:8+7ZpsrTPpx3/5pSY7STDrta0ViqeDOGfI8++qoUXvmDvaGLLaSOAU1tdQxsXUeRj3CMgYthRb6hqlWfLmE6Cg==:"
)

func TestSharedSignatureVector(t *testing.T) {
	_, priv := DefaultKeyPair()
	req := httptest.NewRequest("POST", "/api/orders", nil)
	if err := signWith(req, priv, DefaultDemoAgentKeyID, "agent-payer-auth", "events.sidequestz.tech", 1790000000, 1790000300, "nonce_vector"); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Signature-Input"); got != vectorSignatureInput {
		t.Fatalf("Signature-Input = %s", got)
	}
	if got := req.Header.Get("Signature"); got != vectorSignature {
		t.Fatalf("Signature = %s", got)
	}
}
