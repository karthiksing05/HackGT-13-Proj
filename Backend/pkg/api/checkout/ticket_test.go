package checkout

import (
	"Backend/pkg/models"
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestTicketPageEscapes(t *testing.T) {
	var buf bytes.Buffer
	view := ticketView{Found: true, Title: `<script>alert(1)</script> Jazz & Blues`, When: "Sun, Sep 27 · 5:30–8 PM",
		Where: "Pier Nine", Confirmation: "SQ-4F7K2", Quantity: 2, Total: "$24.00", Card: "Visa •••• 4242"}
	if err := ticketPage.Execute(&buf, view); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	if strings.Contains(page, "<script>") || !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt; Jazz &amp; Blues") {
		t.Fatalf("title not escaped:\n%s", page)
	}
	if path := os.Getenv("SQ_TICKET_PAGE_OUT"); path != "" {
		if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	buf.Reset()
	if err := ticketPage.Execute(&buf, ticketView{}); err != nil || !strings.Contains(buf.String(), "Ticket not found") || strings.Contains(buf.String(), "Demo ticket") {
		t.Fatalf("not-found page: %v\n%s", err, buf.String())
	}
}

func TestMoneyAndConfirmation(t *testing.T) {
	for cents, want := range map[int]string{0: "$0.00", 5: "$0.05", 2400: "$24.00", 1250: "$12.50", -300: "-$3.00"} {
		if got := money(cents); got != want {
			t.Errorf("money(%d) = %q, want %q", cents, got, want)
		}
	}
	code := regexp.MustCompile(`^SQ-[0-9A-HJKMNP-TV-Z]{5}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c := newConfirmation()
		if !code.MatchString(c) {
			t.Fatalf("confirmation %q", c)
		}
		seen[c] = true
	}
	if len(seen) < 195 {
		t.Fatalf("confirmations repeat too often: %d distinct of 200", len(seen))
	}
	if a, b := newTicketID(), newTicketID(); a == b || len(a) != 22 {
		t.Fatalf("ticket ids %q %q", a, b)
	}
}

func TestPickCard(t *testing.T) {
	old := &models.PaymentMethod{ID: "old"}
	def := &models.PaymentMethod{ID: "def", IsDefault: true}
	cards := []*models.PaymentMethod{old, def}
	asked := "old"
	missing := "gone"
	switch {
	case pickCard(cards, nil) != def:
		t.Error("null must pick the default")
	case pickCard(cards, &asked) != old:
		t.Error("the requested card must win")
	case pickCard(cards, &missing) != def:
		t.Error("an unknown card falls back to the default")
	case pickCard([]*models.PaymentMethod{old}, nil) != old:
		t.Error("without a default, the first card")
	case pickCard(nil, &asked) != nil:
		t.Error("no cards, no card")
	}
}
