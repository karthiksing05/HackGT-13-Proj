package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The dev fallback reads the gitignored repo-root meta_app_id /
// meta_app_secret files; they exist on the coordinator's checkout only, so
// their absence must leave Facebook off without failing startup.
func TestDevFacebookFallback(t *testing.T) {
	t.Chdir(t.TempDir())
	c, err := Parse(env(map[string]string{"APP_ENV": "dev"}))
	if err != nil {
		t.Fatalf("dev without the files: %v", err)
	}
	if c.FBAppID != "" || c.FBAppSecret != "" || c.FBGraphVersion != "v26.0" || c.FBTokenKey != "" {
		t.Fatalf("without the files: id %q, secret set %v, version %q", c.FBAppID, c.FBAppSecret != "", c.FBGraphVersion)
	}

	// The server runs from Backend/, so the files sit one level up.
	root := t.TempDir()
	for name, value := range map[string]string{"meta_app_id": "123456\n", "meta_app_secret": "shh-dev-secret\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	backend := filepath.Join(root, "Backend")
	if err := os.Mkdir(backend, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(backend)
	c, err = Parse(env(map[string]string{"APP_ENV": "dev"}))
	if err != nil || c.FBAppID != "123456" || c.FBAppSecret != "shh-dev-secret" {
		t.Fatalf("dev with the files: id %q, secret read %v, %v", c.FBAppID, c.FBAppSecret == "shh-dev-secret", err)
	}
	// The environment wins, and production never reads the files.
	c, err = Parse(env(map[string]string{"APP_ENV": "dev", "FB_APP_ID": "env-id", "FB_APP_SECRET": "env-secret", "FB_GRAPH_VERSION": "v27.0"}))
	if err != nil || c.FBAppID != "env-id" || c.FBAppSecret != "env-secret" || c.FBGraphVersion != "v27.0" {
		t.Fatalf("env over files: %q %v", c.FBAppID, err)
	}
	c, err = Parse(env(map[string]string{"JWT_SECRET": strings.Repeat("x", 32), "PUBLIC_BASE_URL": "https://api.example.test"}))
	if err != nil || c.FBAppID != "" || c.FBAppSecret != "" {
		t.Fatalf("prod read the dev files: %q %v", c.FBAppID, err)
	}
}
