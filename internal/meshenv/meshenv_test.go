package meshenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	content := `# comment
DOMAIN=alice.nsl.sh
export PUBLIC_IP=203.0.113.5
QUOTED="with space"
SINGLE='x'
EMPTY=
PROVIDER=https://api.nsl.sh/,uid123,sig
garbage line
`
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	env, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"DOMAIN": "alice.nsl.sh", "PUBLIC_IP": "203.0.113.5", "QUOTED": "with space", "SINGLE": "x", "EMPTY": "",
	} {
		if env[k] != want {
			t.Errorf("%s = %q, want %q", k, env[k], want)
		}
	}
	if _, ok := env["garbage line"]; ok {
		t.Error("parsed a line without =")
	}

	// PROVIDER_STR absent: falls back to the deprecated PROVIDER.
	p, err := env.Provider()
	if err != nil {
		t.Fatal(err)
	}
	if p.BackendURL != "https://api.nsl.sh" || p.UserID != "uid123" || p.APIBase() != "https://api.nsl.sh/router/api" {
		t.Fatalf("provider = %+v", p)
	}

	env["PROVIDER_STR"] = "https://new.example,uid9,s"
	if p, _ := env.Provider(); p.UserID != "uid9" {
		t.Fatalf("PROVIDER_STR should win, got %+v", p)
	}
	env["PROVIDER_STR"] = "bad"
	env["PROVIDER"] = ""
	if _, err := env.Provider(); err == nil {
		t.Fatal("expected error for malformed provider")
	}
}

func TestDash(t *testing.T) {
	if got := Dash("203.0.113.5"); got != "203-0-113-5" {
		t.Fatal(got)
	}
	if got := Dash("2001:db8::1"); got != "2001-db8--1" {
		t.Fatal(got)
	}
}
