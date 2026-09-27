package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config holds settings for the Events merchant server.
type Config struct {
	AppEnv          string
	HTTPAddr        string
	MerchantHost    string
	MerchantBaseURL string
	PaymentsMode    string
	DemoKey         string
	MongoURI        string
	MongoDB         string

	// TapAgentPublicKey is the SideQuestz agent's Ed25519 public key (base64).
	// Empty only in dev, where the demo key from the repo is accepted.
	TapAgentPublicKey string

	// StripeSecretKey is the merchant's Stripe test key (sk_test_/rk_test_).
	// Empty: orders answer 503 (nothing can be charged).
	StripeSecretKey string
	// StripeAPIBase is Stripe's API host (tests point it at a fake).
	StripeAPIBase string
}

// IsDev reports whether the server runs in development or tests.
func (c *Config) IsDev() bool {
	return c.AppEnv == "dev" || c.AppEnv == "development" || c.AppEnv == "test" || c.AppEnv == ""
}

// FromEnv loads configuration from the environment, setting safe sandbox defaults.
func FromEnv() (*Config, error) {
	c := &Config{
		AppEnv:            env("APP_ENV", "dev"),
		HTTPAddr:          env("HTTP_ADDR", env("PORT", ":8085")),
		MerchantHost:      env("MERCHANT_HOST", "events.sidequestz.tech"),
		MerchantBaseURL:   env("MERCHANT_BASE_URL", "http://localhost:8085"),
		PaymentsMode:      env("PAYMENTS_MODE", "sandbox"),
		DemoKey:           env("DEMO_KEY", "sqz-booth-demo"),
		MongoURI:          env("MONGO_URI", env("MONGODB_URI", "mongodb://127.0.0.1:27017")),
		MongoDB:           env("MONGO_DB", env("MONGODB_DATABASE", "sidequestz_events")),
		TapAgentPublicKey: env("TAP_AGENT_PUBLIC_KEY", ""),
		StripeSecretKey:   env("STRIPE_MERCHANT_SECRET_KEY", env("STRIPE_SECRET_KEY", "")),
		StripeAPIBase:     env("STRIPE_API_BASE", "https://api.stripe.com"),
	}

	// Sandbox only: the server refuses to start in any other mode.
	if c.PaymentsMode != "sandbox" {
		return nil, errors.New("PAYMENTS_MODE must be 'sandbox'")
	}

	// Ensure HTTPAddr has host:port or :port format
	if !strings.Contains(c.HTTPAddr, ":") {
		c.HTTPAddr = ":" + c.HTTPAddr
	}

	// Dynamically align MerchantBaseURL if using localhost default and port changed
	if (c.MerchantBaseURL == "http://localhost:8085" || c.MerchantBaseURL == "") && c.HTTPAddr != ":8085" {
		c.MerchantBaseURL = "http://localhost" + c.HTTPAddr
	}

	// Stripe test keys only: a live key can never be configured.
	if c.StripeSecretKey != "" && !strings.HasPrefix(c.StripeSecretKey, "sk_test_") && !strings.HasPrefix(c.StripeSecretKey, "rk_test_") {
		return nil, errors.New("STRIPE_MERCHANT_SECRET_KEY / STRIPE_SECRET_KEY must be a test key (sk_test_… or rk_test_…)")
	}
	if u, err := url.Parse(c.StripeAPIBase); err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("STRIPE_API_BASE %q is not a URL", c.StripeAPIBase)
	}

	// The demo signing key's seed is in the repo; outside dev a real key is required.
	if c.TapAgentPublicKey == "" && !c.IsDev() {
		return nil, errors.New("TAP_AGENT_PUBLIC_KEY is required when APP_ENV is not dev")
	}
	if !c.IsDev() && c.DemoKey == "sqz-booth-demo" {
		return nil, errors.New("DEMO_KEY must be changed from the default when APP_ENV is not dev")
	}

	return c, nil
}

func env(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
