package me_test

import (
	"Backend/pkg/testutil"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// examplesDir holds the app's contract dumps (docs/api/examples).
const examplesDir = "../../../../docs/api/examples"

// example reads one contract dump.
func example(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(examplesDir, name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// canonical re-serializes JSON with sorted keys so documents compare by value.
func canonical(t *testing.T, raw []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("canonical %q: %v", raw, err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// putJSON sends a raw JSON body.
func putJSON(t *testing.T, srv *testutil.Server, method, path string, raw []byte, sess *testutil.Session) *testutil.Response {
	t.Helper()
	return srv.DoRaw(t, method, path, bytes.NewReader(raw), map[string]string{
		"Content-Type":  "application/json",
		"Authorization": sess.Bearer(),
	})
}
