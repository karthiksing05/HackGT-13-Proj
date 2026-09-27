package config

import (
	"os"
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	os.Setenv("PAYMENTS_MODE", "sandbox")
	cfg, err := FromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PaymentsMode != "sandbox" {
		t.Errorf("expected PaymentsMode=sandbox, got %s", cfg.PaymentsMode)
	}
}

func TestConfigRejectsNonSandbox(t *testing.T) {
	os.Setenv("PAYMENTS_MODE", "production")
	defer os.Setenv("PAYMENTS_MODE", "sandbox")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error when PAYMENTS_MODE != sandbox, got nil")
	}
}

func TestConfigRejectsNonSandboxVisaURL(t *testing.T) {
	os.Setenv("PAYMENTS_MODE", "sandbox")
	os.Setenv("VISA_AUTHORIZE_URL", "https://api.visa.com/v1/pay")
	defer os.Unsetenv("VISA_AUTHORIZE_URL")

	_, err := FromEnv()
	if err == nil {
		t.Fatal("expected error for production visa URL, got nil")
	}
}
