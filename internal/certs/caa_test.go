package certs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeAccount(t *testing.T, dataDir, dir, email, uri, status string) {
	t.Helper()
	p := filepath.Join(dataDir, "caddy", "acme", dir, "users", email)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"status":"` + status + `","location":"` + uri + `","contact":["mailto:` + email + `"]}`
	if err := os.WriteFile(filepath.Join(p, "acct.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// A key file beside it must never be read.
	if err := os.WriteFile(filepath.Join(p, "acct.key"), []byte("PRIVATE"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeState(t *testing.T, dataDir, body string, mod time.Time) {
	t.Helper()
	p := filepath.Join(dataDir, "mesh-caa.state")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestReadCAA(t *testing.T) {
	now := time.Now()
	const prod, staging, other = "https://acme-v02.api.letsencrypt.org/acme/acct/1", "https://acme-staging-v02.api.letsencrypt.org/acme/acct/2", "https://acme-v02.api.letsencrypt.org/acme/acct/9"

	t.Run("all reported", func(t *testing.T) {
		d := t.TempDir()
		writeAccount(t, d, leProdDir, "me@x.io", prod, "valid")
		writeAccount(t, d, leStagingDir, "me@x.io", staging, "valid")
		writeState(t, d, `["`+staging+`","`+prod+`"]`, now.Add(-time.Hour))
		c := ReadCAA(d, "Me@x.io", true, now)
		if len(c.Issues) != 0 || len(c.Accounts) != 2 || !c.Accounts[0].Current || c.Accounts[0].URI != prod || !c.Accounts[0].Reported || c.Accounts[1].Current {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("current account not reported", func(t *testing.T) {
		d := t.TempDir()
		writeAccount(t, d, leProdDir, "me@x.io", prod, "valid")
		writeState(t, d, `["`+other+`"]`, now.Add(-time.Hour))
		c := ReadCAA(d, "me@x.io", true, now)
		if len(c.Issues) != 1 || !strings.Contains(c.Issues[0], "not in the published CAA") {
			t.Fatalf("%+v", c.Issues)
		}
	})
	t.Run("stale report", func(t *testing.T) {
		d := t.TempDir()
		writeAccount(t, d, leProdDir, "me@x.io", prod, "valid")
		writeState(t, d, `["`+prod+`"]`, now.Add(-30*time.Hour))
		c := ReadCAA(d, "me@x.io", true, now)
		if len(c.Issues) != 1 || !strings.Contains(c.Issues[0], "more than a day") {
			t.Fatalf("%+v", c.Issues)
		}
	})
	t.Run("never reported", func(t *testing.T) {
		d := t.TempDir()
		writeAccount(t, d, leProdDir, "me@x.io", prod, "valid")
		c := ReadCAA(d, "me@x.io", true, now)
		if len(c.Issues) != 1 || !strings.Contains(c.Issues[0], "never reported") || c.ReportedAt != nil {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("default user and deactivated account", func(t *testing.T) {
		d := t.TempDir()
		writeAccount(t, d, leProdDir, "default", prod, "valid")
		writeAccount(t, d, leProdDir, "old@x.io", other, "deactivated")
		writeState(t, d, `["`+prod+`"]`, now)
		c := ReadCAA(d, "", true, now)
		if len(c.Accounts) != 1 || !c.Accounts[0].Current || len(c.Issues) != 0 {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("only a staging account", func(t *testing.T) {
		d := t.TempDir()
		writeAccount(t, d, leStagingDir, "me@x.io", staging, "valid")
		writeState(t, d, `["`+staging+`"]`, now)
		c := ReadCAA(d, "me@x.io", true, now)
		if len(c.Issues) != 1 || !strings.Contains(c.Issues[0], "no production account") {
			t.Fatalf("%+v", c.Issues)
		}
	})
	t.Run("not delegated: facts but no issues", func(t *testing.T) {
		d := t.TempDir()
		writeAccount(t, d, leProdDir, "me@x.io", prod, "valid")
		c := ReadCAA(d, "me@x.io", false, now)
		if len(c.Issues) != 0 || len(c.Accounts) != 1 || c.Delegated {
			t.Fatalf("%+v", c)
		}
	})
	t.Run("empty storage", func(t *testing.T) {
		c := ReadCAA(t.TempDir(), "me@x.io", true, now)
		if c.Accounts == nil || c.Reported == nil {
			t.Fatalf("nil lists would serialise as null: %+v", c)
		}
	})
}
