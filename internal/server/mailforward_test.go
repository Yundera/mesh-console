package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yundera/mesh-console/internal/config"
)

func TestMailForwardRelaysSigned(t *testing.T) {
	var seen []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		switch {
		case r.Method == http.MethodPut && strings.Contains(string(body), "alice@nsl.sh"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"The forwarding address cannot be in nsl.sh."}`))
		case r.Method == http.MethodPut:
			_, _ = w.Write([]byte(`{"enabled":true,"address":"alice@nsl.sh","state":"pending","target":"me@gmail.com"}`))
		default:
			_, _ = w.Write([]byte(`{"enabled":true,"address":"alice@nsl.sh","state":"off"}`))
		}
	}))
	defer fake.Close()

	dir := t.TempDir()
	env := "DOMAIN=alice.nsl.sh\nPROVIDER_STR=" + fake.URL + ",uid1,s3cr3t\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newServer(config.Config{DevIdentity: "dev", Env: "development", MeshDir: dir}).routes(fstest.MapFS{})
	write := map[string]string{"X-Mesh-Console": "1"}

	rec := do(h, http.MethodGet, "/api/mail/forward", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"supported":true`) || !strings.Contains(rec.Body.String(), `"state":"off"`) {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "s3cr3t") {
		t.Fatal("the signature leaked into the response")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/mail/forward", strings.NewReader(`{"target":"me@gmail.com"}`))
	req.Header.Set("X-Mesh-Console", "1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var view map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &view)
	if rec.Code != http.StatusOK || view["state"] != "pending" || view["target"] != "me@gmail.com" {
		t.Fatalf("set = %d %s", rec.Code, rec.Body)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/mail/forward", strings.NewReader(`{"target":"alice@nsl.sh"}`))
	req.Header.Set("X-Mesh-Console", "1")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "cannot be in nsl.sh") {
		t.Fatalf("refused = %d %s", rec.Code, rec.Body)
	}

	if rec := do(h, http.MethodDelete, "/api/mail/forward", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("delete without the CSRF header = %d, want 403", rec.Code)
	}
	if rec := do(h, http.MethodDelete, "/api/mail/forward", write); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}

	want := []string{
		"GET /router/api/email/forward/uid1/s3cr3t ",
		`PUT /router/api/email/forward/uid1/s3cr3t {"target":"me@gmail.com"}`,
		`PUT /router/api/email/forward/uid1/s3cr3t {"target":"alice@nsl.sh"}`,
		"DELETE /router/api/email/forward/uid1/s3cr3t ",
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("backend saw:\n%s", strings.Join(seen, "\n"))
	}
}

func TestMailForwardOldBackend(t *testing.T) {
	fake := httptest.NewServer(http.NotFoundHandler())
	defer fake.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("PROVIDER_STR="+fake.URL+",uid1,sig\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newServer(config.Config{DevIdentity: "dev", Env: "development", MeshDir: dir}).routes(fstest.MapFS{})
	rec := do(h, http.MethodGet, "/api/mail/forward", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"supported":false`) {
		t.Fatalf("old backend = %d %s", rec.Code, rec.Body)
	}
}
