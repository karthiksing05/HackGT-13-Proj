// Package config reads the typed server configuration from the environment
// (docs/design/backend-contract.md §8) and refuses to start with unsafe values.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config is the whole server configuration. Every field has an env name in
// Parse; tests build one directly.
type Config struct {
	AppEnv        string // "prod" (default) or "dev"
	HTTPAddr      string // HTTP_ADDR, default 127.0.0.1:8080 (PORT accepted as ":<port>")
	PublicBaseURL string // PUBLIC_BASE_URL, no trailing slash
	MongoURI      string
	MongoDB       string

	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration

	MLServiceURL        string
	MLRerank            bool
	MLRerankTopK        int
	MLRankTimeout       time.Duration
	MLRankRerankTimeout time.Duration
	MLEmbedTimeout      time.Duration
	MLSearchTimeout     time.Duration

	Planner string // PLANNER: auto | dag | legacy (pkg/planner reads its own PLANNER_* knobs)

	FBAppID        string
	FBAppSecret    string
	FBGraphVersion string
	FBTokenKey     string // 32-byte hex for AES-256-GCM; empty = HKDF from JWTSecret (facebook agent)

	DemoPassword string
	DemoTZ       string

	DevResetCodes     bool          // DEV_RESET_CODES=1 returns the reset code in the forgot response
	CheckoutStepDelay time.Duration // CHECKOUT_STEP_DELAY, 1.5 s (tests use 0)
	TrustProxy        bool          // TRUST_PROXY=1 reads X-Forwarded-For for the client IP
	MaxPhotoBytes     int64
	MaxJSONBytes      int64

	// DisableRateLimits switches the /auth rate limiters off (tests only; DISABLE_RATE_LIMITS=1).
	DisableRateLimits bool
}

// Placeholder secrets that must never reach production.
var placeholderSecrets = map[string]bool{
	"sidequestz-super-secret-jwt-key-2026": true,
	"JWT_SECRET":                           true,
	"change-me":                            true,
	"changeme":                             true,
	"secret":                               true,
}

const minSecretBytes = 32

// FromEnv parses the process environment and validates it.
func FromEnv() (*Config, error) {
	return Parse(os.Getenv)
}

// Parse builds a Config from a getenv function and validates it.
func Parse(getenv func(string) string) (*Config, error) {
	var errs []error
	get := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	integer := func(key string, def int) int {
		v := get(key, "")
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
			return def
		}
		return n
	}
	dur := func(key, def string) time.Duration {
		d, err := time.ParseDuration(get(key, def))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", key, err))
		}
		return d
	}
	millis := func(key string, def int) time.Duration {
		return time.Duration(integer(key, def)) * time.Millisecond
	}

	c := &Config{
		AppEnv:              get("APP_ENV", "prod"),
		HTTPAddr:            get("HTTP_ADDR", ""),
		PublicBaseURL:       strings.TrimRight(get("PUBLIC_BASE_URL", ""), "/"),
		MongoURI:            get("MONGO_URI", "mongodb://127.0.0.1:27017"),
		MongoDB:             get("MONGO_DB", "freetime"),
		JWTSecret:           get("JWT_SECRET", ""),
		AccessTokenTTL:      dur("ACCESS_TOKEN_TTL", "1h"),
		RefreshTokenTTL:     dur("REFRESH_TOKEN_TTL", "720h"),
		MLServiceURL:        strings.TrimRight(get("ML_SERVICE_URL", get("ML_API_URL", "http://127.0.0.1:8000")), "/"),
		MLRerank:            boolean(get("ML_RERANK", "true")),
		MLRerankTopK:        integer("ML_RERANK_TOP_K", 12),
		MLRankTimeout:       millis("ML_RANK_TIMEOUT_MS", 2000),
		MLRankRerankTimeout: millis("ML_RANK_RERANK_TIMEOUT_MS", 20000),
		MLEmbedTimeout:      millis("ML_EMBED_TIMEOUT_MS", 15000),
		MLSearchTimeout:     millis("ML_SEARCH_TIMEOUT_MS", 5000),
		Planner:             get("PLANNER", "auto"),
		FBAppID:             get("FB_APP_ID", ""),
		FBAppSecret:         get("FB_APP_SECRET", ""),
		FBGraphVersion:      get("FB_GRAPH_VERSION", "v26.0"),
		FBTokenKey:          get("FB_TOKEN_KEY", ""),
		DemoPassword:        get("DEMO_PASSWORD", ""),
		DemoTZ:              get("DEMO_TZ", "America/New_York"),
		DevResetCodes:       boolean(get("DEV_RESET_CODES", "0")),
		CheckoutStepDelay:   dur("CHECKOUT_STEP_DELAY", "1500ms"),
		TrustProxy:          boolean(get("TRUST_PROXY", "0")),
		MaxPhotoBytes:       int64(integer("MAX_PHOTO_BYTES", 2<<20)),
		MaxJSONBytes:        int64(integer("MAX_JSON_BYTES", 1<<20)),
		DisableRateLimits:   boolean(get("DISABLE_RATE_LIMITS", "0")),
	}

	if c.HTTPAddr == "" {
		if port := get("PORT", ""); port != "" {
			c.HTTPAddr = ":" + strings.TrimPrefix(port, ":")
		} else {
			c.HTTPAddr = "127.0.0.1:8080"
		}
	}
	if c.Dev() {
		if c.PublicBaseURL == "" {
			c.PublicBaseURL = "http://" + localHost(c.HTTPAddr)
		}
		if c.FBAppID == "" {
			c.FBAppID, c.FBAppSecret = repoRootFacebookFiles()
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Dev reports whether the relaxed development rules apply (APP_ENV=dev).
func (c *Config) Dev() bool { return c.AppEnv == "dev" }

// Validate refuses unsafe or incomplete settings. In dev a missing JWT secret
// is replaced by a random one (sessions then end at every restart).
func (c *Config) Validate() error {
	var errs []error
	switch c.AppEnv {
	case "prod", "dev":
	default:
		errs = append(errs, fmt.Errorf("APP_ENV must be prod or dev, got %q", c.AppEnv))
	}
	if c.JWTSecret == "" && c.Dev() {
		c.JWTSecret = randomSecret()
	}
	switch {
	case c.JWTSecret == "":
		errs = append(errs, errors.New("JWT_SECRET is required (at least 32 bytes)"))
	case placeholderSecrets[c.JWTSecret] && !c.Dev():
		errs = append(errs, errors.New("JWT_SECRET is a placeholder value; set a real secret"))
	case len(c.JWTSecret) < minSecretBytes && !c.Dev():
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d bytes", minSecretBytes))
	}
	if c.PublicBaseURL == "" {
		errs = append(errs, errors.New("PUBLIC_BASE_URL is required"))
	} else if !strings.HasPrefix(c.PublicBaseURL, "http://") && !strings.HasPrefix(c.PublicBaseURL, "https://") {
		errs = append(errs, fmt.Errorf("PUBLIC_BASE_URL must start with http:// or https://, got %q", c.PublicBaseURL))
	}
	if c.MongoURI == "" {
		errs = append(errs, errors.New("MONGO_URI is required"))
	}
	if c.MongoDB == "" {
		errs = append(errs, errors.New("MONGO_DB is required"))
	}
	if c.AccessTokenTTL <= 0 || c.RefreshTokenTTL <= 0 {
		errs = append(errs, errors.New("ACCESS_TOKEN_TTL and REFRESH_TOKEN_TTL must be positive"))
	}
	switch c.Planner {
	case "auto", "dag", "legacy":
	default:
		errs = append(errs, fmt.Errorf("PLANNER must be auto, dag or legacy, got %q", c.Planner))
	}
	if c.MaxPhotoBytes <= 0 || c.MaxJSONBytes <= 0 {
		errs = append(errs, errors.New("MAX_PHOTO_BYTES and MAX_JSON_BYTES must be positive"))
	}
	if c.CheckoutStepDelay < 0 {
		errs = append(errs, errors.New("CHECKOUT_STEP_DELAY must not be negative"))
	}
	return errors.Join(errs...)
}

func boolean(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// localHost turns a listen address into something a browser on this machine can reach.
func localHost(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "127.0.0.1" + addr
	}
	return strings.Replace(addr, "0.0.0.0", "127.0.0.1", 1)
}

func randomSecret() string {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		panic("config: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// repoRootFacebookFiles reads the gitignored meta_app_id / meta_app_secret files
// from the repository root (the process runs from Backend/ or the root) for dev.
func repoRootFacebookFiles() (id, secret string) {
	for _, dir := range []string{".", "..", "../.."} {
		idBytes, err := os.ReadFile(filepath.Join(dir, "meta_app_id"))
		if err != nil {
			continue
		}
		secretBytes, err := os.ReadFile(filepath.Join(dir, "meta_app_secret"))
		if err != nil {
			continue
		}
		return strings.TrimSpace(string(idBytes)), strings.TrimSpace(string(secretBytes))
	}
	return "", ""
}
