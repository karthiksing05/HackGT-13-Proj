package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Config holds settings for the Events merchant server.
type Config struct {
	AppEnv            string
	HTTPAddr          string
	MerchantHost      string
	MerchantBaseURL   string
	PaymentsMode      string
	DemoKey           string
	SandboxNetworkKey string
	VisaAuthorizeURL  string
	MongoURI          string
	MongoDB           string
	TapAgentKey       string // Base64-encoded Ed25519 seed or private key
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
		SandboxNetworkKey: env("SANDBOX_NETWORK_KEY", "sqz-sbx-network-key-hackgt"),
		VisaAuthorizeURL:  env("VISA_AUTHORIZE_URL", ""),
		MongoURI:          env("MONGO_URI", env("MONGODB_URI", "mongodb://127.0.0.1:27017")),
		MongoDB:           env("MONGO_DB", env("MONGODB_DATABASE", "sidequestz_events")),
		TapAgentKey:       env("TAP_AGENT_KEY", ""),
	}

	// Guarantee sandbox mode per spec:
	// "PAYMENTS_MODE=sandbox is required. config.FromEnv fails without it and rejects any Visa base URL that isn't a sandbox host."
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

	// If a custom Visa URL is provided, reject any host that isn't a sandbox host
	if c.VisaAuthorizeURL != "" {
		u, err := url.Parse(c.VisaAuthorizeURL)
		if err != nil {
			return nil, fmt.Errorf("invalid VISA_AUTHORIZE_URL: %w", err)
		}
		host := strings.ToLower(u.Hostname())
		if host != "localhost" && host != "127.0.0.1" && !strings.Contains(host, "sandbox") && !strings.Contains(host, "sidequestz") {
			return nil, fmt.Errorf("VISA_AUTHORIZE_URL host %q is not an authorized sandbox host", host)
		}
	}

	// Validate TapAgentKey base64 if provided
	if c.TapAgentKey != "" {
		if _, err := base64.StdEncoding.DecodeString(c.TapAgentKey); err != nil {
			return nil, fmt.Errorf("invalid TAP_AGENT_KEY base64: %w", err)
		}
	}

	return c, nil
}

func env(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
