package payments

import (
	"context"
	"fmt"
	"sync"
)

// Fake is an in-memory Issuer for tests: it records every token it issues
// and every revoke.
type Fake struct {
	mu      sync.Mutex
	Issued  []IssueRequest
	Tokens  []string
	Revoked []string
	// Decline makes Issue fail with ErrDeclined (a declined test card).
	Decline bool
}

// Issue implements Issuer.
func (f *Fake) Issue(_ context.Context, req IssueRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Decline || req.PaymentMethod == "pm_card_chargeDeclined" {
		return "", ErrDeclined
	}
	f.Issued = append(f.Issued, req)
	spt := fmt.Sprintf("spt_fake_%d", len(f.Issued))
	f.Tokens = append(f.Tokens, spt)
	return spt, nil
}

// Revoke implements Issuer.
func (f *Fake) Revoke(_ context.Context, spt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Revoked = append(f.Revoked, spt)
	return nil
}

// Last is the most recent issue request.
func (f *Fake) Last() (IssueRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Issued) == 0 {
		return IssueRequest{}, false
	}
	return f.Issued[len(f.Issued)-1], true
}
