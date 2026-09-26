package integrations_test

import (
	"Backend/pkg/testutil"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// browser requests a hosted page the way the app's web sheet does: no bearer
// token and no redirect following, so a 302 into sidequestz:// can be asserted.
// A non-nil form is posted url-encoded.
func browser(t *testing.T, srv *testutil.Server, method, path string, form url.Values) *testutil.Response {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, srv.URL(path), body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := *srv.HTTP.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return &testutil.Response{Status: res.StatusCode, Header: res.Header, Body: raw}
}

// hostedLink splits a returned page URL into its server path and its token,
// checking it points at this server.
func hostedLink(t *testing.T, srv *testutil.Server, raw, wantPath string) (path, token string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if base := u.Scheme + "://" + u.Host; base != srv.Cfg.PublicBaseURL || u.Path != wantPath {
		t.Fatalf("link %s: want %s%s?t=…", raw, srv.Cfg.PublicBaseURL, wantPath)
	}
	token = u.Query().Get("t")
	if len(token) < 40 {
		t.Fatalf("link token too short: %q", token)
	}
	return u.RequestURI(), token
}

// expectPageHeaders checks the headers every hosted page carries.
func expectPageHeaders(t *testing.T, res *testutil.Response) {
	t.Helper()
	h := res.Header
	if !strings.HasPrefix(h.Get("Content-Type"), "text/html") || h.Get("Cache-Control") != "no-store" ||
		h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "no-referrer" ||
		!strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") ||
		!strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("page headers: %v", h)
	}
}

// canonical re-serializes JSON with sorted keys.
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
