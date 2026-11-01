package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionFrontendAndAPIShareRouterWithoutExposingOtherFiles(t *testing.T) {
	base := t.TempDir()
	dist := filepath.Join(base, "dist")
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	const html = "<!doctype html><html><body>WalletLedger synthetic static test</body></html>"
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(html), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "test.js"), []byte("window.syntheticWalletStatic = true;"), 0600); err != nil {
		t.Fatal(err)
	}
	const outside = "synthetic-outside-dist-secret-947"
	if err := os.WriteFile(filepath.Join(base, ".env"), []byte(outside), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WALLETLEDGER_FRONTEND_DIR", dist)
	f := fixture(t)
	r := f.request(f.admin, "GET", "/", "", nil, nil)
	requireStatus(t, r, 200)
	if string(r.body) != html || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
		t.Fatal("production index was not served as HTML")
	}
	r = f.request(f.admin, "HEAD", "/", "", nil, nil)
	requireStatus(t, r, 200)
	if len(r.body) != 0 {
		t.Fatal("HEAD returned a response body")
	}
	r = f.request(f.admin, "GET", "/assets/test.js", "", nil, nil)
	requireStatus(t, r, 200)
	if !strings.Contains(string(r.body), "syntheticWalletStatic") {
		t.Fatal("production asset was not served")
	}
	r = f.request(f.admin, "GET", "/api/health", "", nil, nil)
	requireStatus(t, r, 200)
	if !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") || decode[map[string]string](t, r)["status"] != "ready" {
		t.Fatal("static handler shadowed API readiness")
	}
	requireStatus(t, f.request(f.admin, "GET", "/api/not-an-endpoint", "", nil, nil), 404)
	requireStatus(t, f.request(f.admin, "GET", "/assets/not-found.js", "", nil, nil), 404)
	r = f.request(f.admin, "POST", "/assets/test.js", "", map[string]any{}, nil)
	requireStatus(t, r, 405)
	if r.header.Get("Allow") != "GET, HEAD" {
		t.Fatal("static method rejection omitted supported methods")
	}
	for _, path := range []string{"/.env", "/%2e%2e/.env", "/assets/%2e%2e/%2e%2e/.env"} {
		r = f.request(f.admin, "GET", path, "", nil, nil)
		requireStatus(t, r, 404)
		if strings.Contains(string(r.body), outside) {
			t.Fatal("static handler disclosed a file outside its dist directory")
		}
	}
	f.consistent(t)
}
