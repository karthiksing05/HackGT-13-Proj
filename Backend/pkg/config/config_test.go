package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseProdRefusesMissingOrPlaceholderSecret(t *testing.T) {
	base := map[string]string{"PUBLIC_BASE_URL": "https://api.example.test"}
	if _, err := Parse(env(base)); err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("missing secret accepted: %v", err)
	}
	base["JWT_SECRET"] = "sidequestz-super-secret-jwt-key-2026"
	if _, err := Parse(env(base)); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("placeholder secret accepted: %v", err)
	}
	base["JWT_SECRET"] = "short"
	if _, err := Parse(env(base)); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("short secret accepted: %v", err)
	}
	base["JWT_SECRET"] = strings.Repeat("x", 32)
	c, err := Parse(env(base))
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if c.HTTPAddr != "127.0.0.1:8080" || c.MongoDB != "freetime" || c.AccessTokenTTL.Hours() != 1 {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestParseProdRequiresPublicBaseURL(t *testing.T) {
	_, err := Parse(env(map[string]string{"JWT_SECRET": strings.Repeat("x", 32)}))
	if err == nil || !strings.Contains(err.Error(), "PUBLIC_BASE_URL") {
		t.Fatalf("missing PUBLIC_BASE_URL accepted: %v", err)
	}
}

func TestParseDevRelaxesSecretAndBaseURL(t *testing.T) {
	c, err := Parse(env(map[string]string{"APP_ENV": "dev", "PORT": "9090"}))
	if err != nil {
		t.Fatalf("dev config rejected: %v", err)
	}
	if len(c.JWTSecret) < minSecretBytes {
		t.Fatalf("dev did not generate a secret: %q", c.JWTSecret)
	}
	if c.HTTPAddr != ":9090" || c.PublicBaseURL != "http://127.0.0.1:9090" {
		t.Fatalf("PORT fallback wrong: %q %q", c.HTTPAddr, c.PublicBaseURL)
	}
	if !c.Dev() {
		t.Fatal("Dev() false")
	}
}

func TestParseRejectsBadValues(t *testing.T) {
	m := map[string]string{"APP_ENV": "dev", "ML_RANK_TIMEOUT_MS": "soon", "CHECKOUT_STEP_DELAY": "later", "PLANNER": "magic"}
	_, err := Parse(env(m))
	if err == nil {
		t.Fatal("bad values accepted")
	}
	for _, want := range []string{"ML_RANK_TIMEOUT_MS", "CHECKOUT_STEP_DELAY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
	m = map[string]string{"APP_ENV": "dev", "PLANNER": "magic"}
	if _, err := Parse(env(m)); err == nil || !strings.Contains(err.Error(), "PLANNER") {
		t.Fatalf("bad PLANNER accepted: %v", err)
	}
}
