package backend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMailForward(t *testing.T) {
	var got struct{ method, path, body string }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(strings.Builder)
		_, _ = io.Copy(b, r.Body)
		got.method, got.path, got.body = r.Method, r.URL.EscapedPath(), b.String()
		switch {
		case strings.Contains(r.URL.Path, "/old/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && strings.Contains(got.body, "bad"):
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"Enter a valid email address."}`))
		default:
			_ = json.NewEncoder(w).Encode(MailForward{Enabled: true, Address: "alice@nsl.sh", State: "pending", Target: "me@gmail.com"})
		}
	}))
	defer srv.Close()
	c := New(srv.URL)
	ctx := context.Background()

	f, err := c.SetMailForward(ctx, "uid/1", "s1g", "me@gmail.com")
	if err != nil || f.State != "pending" || f.Target != "me@gmail.com" {
		t.Fatalf("set: %+v %v", f, err)
	}
	if got.method != http.MethodPut || got.path != "/email/forward/uid%2F1/s1g" || got.body != `{"target":"me@gmail.com"}` {
		t.Fatalf("request: %+v", got)
	}

	if _, err := c.ClearMailForward(ctx, "u", "s"); err != nil || got.method != http.MethodDelete {
		t.Fatalf("clear: %v %+v", err, got)
	}

	_, err = c.SetMailForward(ctx, "u", "secret-sig", "bad")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 || apiErr.Message != "Enter a valid email address." {
		t.Fatalf("refusal: %v", err)
	}

	if _, err := c.MailForward(ctx, "old", "s"); !errors.Is(err, ErrMailForwardUnsupported) {
		t.Fatalf("old backend: %v", err)
	}

	// The signature is in the URL: an unreachable backend must not echo it.
	dead := New("http://127.0.0.1:1")
	if _, err := dead.MailForward(ctx, "u", "secret-sig"); err == nil || strings.Contains(err.Error(), "secret-sig") {
		t.Fatalf("unreachable error leaks the signature or is nil: %v", err)
	}
}
