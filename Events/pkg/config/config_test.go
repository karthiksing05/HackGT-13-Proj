package config

import (
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("PAYMENTS_MODE", "sandbox")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PaymentsMode != "sandbox" || !cfg.IsDev() {
		t.Errorf("got PaymentsMode=%s dev=%v", cfg.PaymentsMode, cfg.IsDev())
	}
}

func TestConfigRejectsNonSandbox(t *testing.T) {
	t.Setenv("PAYMENTS_MODE", "production")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error when PAYMENTS_MODE != sandbox, got nil")
	}
}

func TestConfigRejectsLiveStripeKey(t *testing.T) {
	t.Setenv("STRIPE_SECRET_KEY", "sk_live_abc")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error for a live Stripe key, got nil")
	}
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_abc")
	if _, err := FromEnv(); err != nil {
		t.Fatalf("test key rejected: %v", err)
	}
}

func TestConfigProdNeedsRealKeys(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error without TAP_AGENT_PUBLIC_KEY in production")
	}
	t.Setenv("TAP_AGENT_PUBLIC_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected error with the default DEMO_KEY in production")
	}
	t.Setenv("DEMO_KEY", "booth-secret")
	if _, err := FromEnv(); err != nil {
		t.Fatalf("production config rejected: %v", err)
	}
}
