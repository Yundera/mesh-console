package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yundera/mesh-console/internal/config"
)

func testServer(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()
	ui := fstest.MapFS{
		"index.html":      &fstest.MapFile{Data: []byte("<!-- spa -->")},
		"assets/app-1.js": &fstest.MapFile{Data: []byte("js")},
	}
	return New(cfg, ui)
}

func meshFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	env := "DOMAIN=alice.nsl.sh\nPUBLIC_IP=203.0.113.5\nPUBLIC_IP_DASH=203-0-113-5\nDEFAULT_SERVICE_HOST=maison\nDEFAULT_SERVICE_PORT=80\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func do(h http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthIsPublic(t *testing.T) {
	h := testServer(t, config.Config{AssertionSecret: "s", AssertionAudience: "mesh-console"})
	if rec := do(h, http.MethodGet, "/api/health", nil); rec.Code != http.StatusOK {
		t.Fatalf("health = %d", rec.Code)
	}
}

func TestAPIRequiresAssertion(t *testing.T) {
	h := testServer(t, config.Config{AssertionSecret: "s", AssertionAudience: "mesh-console"})
	for _, p := range []string{"/api/overview", "/api/routing", "/api/update", "/api/selfcheck", "/api/domain", "/api/stack", "/api/nope"} {
		if rec := do(h, http.MethodGet, p, nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", p, rec.Code)
		}
	}
	if rec := do(h, http.MethodPost, "/api/update/run", map[string]string{"X-Mesh-Console": "1"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("update/run = %d, want 401", rec.Code)
	}
}

// DEV_IDENTITY must never apply outside development, and never alongside a secret.
func TestDevIdentityGuard(t *testing.T) {
	for _, cfg := range []config.Config{
		{DevIdentity: "dev", Env: "production"},
		{DevIdentity: "dev", Env: "development", AssertionSecret: "s"},
	} {
		h := testServer(t, cfg)
		if rec := do(h, http.MethodGet, "/api/me", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("cfg %+v: /api/me = %d, want 401", cfg, rec.Code)
		}
	}
}

func TestCSRFGuardAndOverview(t *testing.T) {
	h := testServer(t, config.Config{DevIdentity: "dev", Env: "development", MeshDir: meshFixture(t), MeshHostRoot: "/DATA/AppData/mesh"})

	if rec := do(h, http.MethodPost, "/api/update/run", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("POST without header = %d, want 403", rec.Code)
	}
	rec := do(h, http.MethodGet, "/api/overview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview = %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{`"domain":"alice.nsl.sh"`, `"domainName":"alice"`, `"serverDomain":"nsl.sh"`, `"dashboard":"https://nsl.sh/dashboard"`, `"sslip":"https://203-0-113-5.sslip.io"`} {
		if !strings.Contains(body, want) {
			t.Errorf("overview missing %s: %s", want, body)
		}
	}
	rec = do(h, http.MethodPost, "/api/domain/default-app", map[string]string{"X-Mesh-Console": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("default-app with empty body = %d, want 400", rec.Code)
	}
}

func TestSPA(t *testing.T) {
	h := testServer(t, config.Config{})
	if rec := do(h, http.MethodGet, "/update", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spa") {
		t.Fatalf("deep link = %d %q", rec.Code, rec.Body)
	}
	rec := do(h, http.MethodGet, "/assets/app-1.js", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset = %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}
