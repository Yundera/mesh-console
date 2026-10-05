package server

import (
	"context"
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

// withTool gives a fixture the template's default-app action, as a template
// that ships it would have (the console looks for it under its /mesh mount).
func withTool(t *testing.T, dir, scriptsRel string) string {
	t.Helper()
	tool := filepath.Join(dir, scriptsRel, "tools", "set-default-app.sh")
	if err := os.MkdirAll(filepath.Dir(tool), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("#!/bin/bash\n"), 0o755); err != nil {
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
	for _, p := range []string{"/api/status", "/api/mail", "/api/overview", "/api/routing", "/api/update", "/api/selfcheck", "/api/domain", "/api/stack", "/api/nope"} {
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
	h := testServer(t, config.Config{DevIdentity: "dev", Env: "development", MeshDir: withTool(t, meshFixture(t), "scripts"), MeshHostRoot: "/DATA/AppData/mesh", DefaultAppEdit: true})

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

func TestDefaultAppEditDisabled(t *testing.T) {
	h := testServer(t, config.Config{DevIdentity: "dev", Env: "development", MeshDir: meshFixture(t), MeshHostRoot: "/DATA/AppData/mesh"})
	rec := do(h, http.MethodPost, "/api/domain/default-app", map[string]string{"X-Mesh-Console": "1"})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "DEFAULT_APP_EDIT") {
		t.Fatalf("disabled edit = %d %s", rec.Code, rec.Body)
	}
}

// A template without tools/set-default-app.sh gets a read-only editor with the
// reason, never a verb that runs a missing script. The Yundera layout (scripts
// under template/, below the mounted root) is found the same way.
func TestDefaultAppNeedsTemplateTool(t *testing.T) {
	h := testServer(t, config.Config{DevIdentity: "dev", Env: "development", MeshDir: meshFixture(t), MeshHostRoot: "/DATA/AppData/mesh", DefaultAppEdit: true})
	rec := do(h, http.MethodPost, "/api/domain/default-app", map[string]string{"X-Mesh-Console": "1"})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "set-default-app.sh") {
		t.Fatalf("missing tool = %d %s", rec.Code, rec.Body)
	}
	rec = do(h, http.MethodGet, "/api/domain", nil)
	if !strings.Contains(rec.Body.String(), `"editable":false`) || !strings.Contains(rec.Body.String(), "set-default-app.sh") {
		t.Fatalf("domain = %s", rec.Body)
	}

	ynd := config.Config{DevIdentity: "dev", Env: "development", MeshHostRoot: "/DATA/AppData/yundera",
		TemplateScripts: "/DATA/AppData/yundera/template/scripts", DefaultAppEdit: true}
	ynd.MeshDir = withTool(t, meshFixture(t), "template/scripts")
	h = testServer(t, ynd)
	rec = do(h, http.MethodGet, "/api/domain", nil)
	if !strings.Contains(rec.Body.String(), `"editable":true`) {
		t.Fatalf("yundera layout domain = %s", rec.Body)
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

type fakeMailer struct{ to []string }

func (f *fakeMailer) Send(_ context.Context, to string) error {
	f.to = append(f.to, to)
	return nil
}

func TestMailTestRecipientIsFixed(t *testing.T) {
	dir := meshFixture(t)
	cfg := config.Config{DevIdentity: "dev", Env: "development", MeshDir: dir}
	s := newServer(cfg)
	fm := &fakeMailer{}
	s.mailer = fm
	h := s.routes(fstest.MapFS{})
	post := map[string]string{"X-Mesh-Console": "1"}

	if rec := do(h, http.MethodPost, "/api/mail/test", post); rec.Code != http.StatusConflict {
		t.Fatalf("no EMAIL = %d %s, want 409", rec.Code, rec.Body)
	}
	env := "DOMAIN=alice.nsl.sh\nEMAIL=owner@example.com\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	// A body naming another recipient is ignored.
	req := httptest.NewRequest(http.MethodPost, "/api/mail/test", strings.NewReader(`{"to":"victim@example.org"}`))
	req.Header.Set("X-Mesh-Console", "1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(fm.to) != 1 || fm.to[0] != "owner@example.com" {
		t.Fatalf("send = %d %s, sent to %v", rec.Code, rec.Body, fm.to)
	}
	if rec := do(h, http.MethodPost, "/api/mail/test", post); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second send = %d, want 429", rec.Code)
	}
}

func TestStatus(t *testing.T) {
	h := testServer(t, config.Config{DevIdentity: "dev", Env: "development", MeshDir: meshFixture(t)})
	rec := do(h, http.MethodGet, "/api/status", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{`"domain":"alice.nsl.sh"`, `"summary":`, `"reachability":`, `"email":`} {
		if !strings.Contains(body, want) {
			t.Errorf("status missing %s: %s", want, body)
		}
	}
}

// The page reads what migrate.sh leaves in the mesh root; a template without
// the tool gets the reason and no verb runs.
func TestMigration(t *testing.T) {
	dir := meshFixture(t)
	mig := filepath.Join(dir, "data", "migrate")
	if err := os.MkdirAll(filepath.Join(mig, "arrived"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(mig, "id_ed25519.pub"), []byte("ssh-ed25519 AAAA mesh-migrate@alice\n"), 0o644)
	os.WriteFile(filepath.Join(mig, "status.json"), []byte(`{"version":1,"phase":"running","steps":[]}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(mig, "arrived", "status.json"), []byte(`{"truncated`), 0o644)
	os.WriteFile(filepath.Join(mig, "migrate.log"), []byte("[2026-10-02 10:00:00] [INFO] hello\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".env"), []byte("DOMAIN=alice.nsl.sh\nMESH_ROUTING_HOLD=retired:203.0.113.9\n"), 0o600)

	h := testServer(t, config.Config{DevIdentity: "dev", Env: "development", MeshDir: dir, MeshHostRoot: "/DATA/AppData/mesh"})
	rec := do(h, http.MethodGet, "/api/migration", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, body)
	}
	for _, want := range []string{`"available":false`, `"key":"ssh-ed25519 AAAA mesh-migrate@alice"`, `"status":{"version":1,"phase":"running","steps":[]}`, `"hold":"retired:203.0.113.9"`, `hello`} {
		if !strings.Contains(body, want) {
			t.Errorf("GET body lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"arrived"`) {
		t.Errorf("invalid arrived/status.json passed through: %s", body)
	}
	rec = do(h, http.MethodPost, "/api/migration/start", map[string]string{"X-Mesh-Console": "1"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("start without the tool = %d %s", rec.Code, rec.Body)
	}

	// With the tool: a bad target is refused before anything runs.
	tool := filepath.Join(dir, "scripts", "tools", "migrate.sh")
	os.MkdirAll(filepath.Dir(tool), 0o755)
	os.WriteFile(tool, []byte("#!/bin/bash\n"), 0o755)
	req := httptest.NewRequest(http.MethodPost, "/api/migration/start", strings.NewReader(`{"target":"u@a;b"}`))
	req.Header.Set("X-Mesh-Console", "1")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad target = %d %s", rr.Code, rr.Body)
	}
}

func TestLastJSONLine(t *testing.T) {
	out := "SSH to h failed (attempt 1/8), retrying in 10s\n{\"ok\":true,\"checks\":[]}\n"
	if got := string(lastJSONLine(out)); got != `{"ok":true,"checks":[]}` {
		t.Fatalf("got %q", got)
	}
	if lastJSONLine("no verdict\n{broken") != nil {
		t.Fatal("broken JSON accepted")
	}
}

// The channel picker needs the template's own action, like the default-app
// editor; with it, input is still validated before any runner starts.
func TestUpdateChannelNeedsTemplateTool(t *testing.T) {
	cfg := config.Config{DevIdentity: "dev", Env: "development", MeshDir: meshFixture(t), MeshHostRoot: "/DATA/AppData/mesh"}
	h := testServer(t, cfg)
	rec := do(h, http.MethodPost, "/api/update/channel", map[string]string{"X-Mesh-Console": "1"})
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "set-update-channel.sh") {
		t.Fatalf("missing tool = %d %s", rec.Code, rec.Body)
	}

	tool := filepath.Join(cfg.MeshDir, "scripts", "tools", "set-update-channel.sh")
	if err := os.MkdirAll(filepath.Dir(tool), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rec = do(h, http.MethodPost, "/api/update/channel", map[string]string{"X-Mesh-Console": "1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty channel = %d %s, want 400", rec.Code, rec.Body)
	}
}
