package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRepo(t *testing.T) {
	cases := map[string]struct {
		repo Repo
		ok   bool
	}{
		"": {Repo{"yundera", "mesh-router-template-root", "stable"}, true},
		"https://github.com/yundera/mesh-router-template-root/archive/refs/heads/main.tar.gz": {Repo{"yundera", "mesh-router-template-root", "main"}, true},
		"https://github.com/me/fork/archive/refs/heads/feat/x.tar.gz":                         {Repo{"me", "fork", "feat/x"}, true},
		"https://codeload.github.com/yundera/mesh-router-template-root/tar.gz/refs/heads/dev": {Repo{"yundera", "mesh-router-template-root", "dev"}, true},
		"file:///tmp/tree.tar.gz":         {Repo{}, false},
		"https://example.com/tree.tar.gz": {Repo{}, false},
	}
	for in, want := range cases {
		got, ok := ParseRepo(in)
		if ok != want.ok || got != want.repo {
			t.Errorf("ParseRepo(%q) = %+v,%v want %+v,%v", in, got, ok, want.repo, want.ok)
		}
	}
}

func TestCompare(t *testing.T) {
	sha := "abc123"
	other := "def456"
	if Compare(nil, &Latest{Commit: sha}) != Unknown {
		t.Error("missing marker must be unknown")
	}
	if Compare(&Revision{Commit: nil}, &Latest{Commit: sha}) != Unknown {
		t.Error("marker without commit must be unknown")
	}
	if Compare(&Revision{Commit: &sha}, nil) != Unknown {
		t.Error("missing latest must be unknown")
	}
	if Compare(&Revision{Commit: &sha}, &Latest{Commit: "ABC123"}) != UpToDate {
		t.Error("same sha must be up to date")
	}
	if Compare(&Revision{Commit: &other}, &Latest{Commit: sha}) != Outdated {
		t.Error("different sha must be outdated")
	}
}

func TestReadRevision(t *testing.T) {
	dir := t.TempDir()
	if r, err := ReadRevision(dir); r != nil || err != nil {
		t.Fatalf("missing marker: %v %v", r, err)
	}
	os.MkdirAll(filepath.Join(dir, "template"), 0o755)
	os.WriteFile(filepath.Join(dir, "template", ".revision.json"), []byte(`{"url":"u","commit":"abc","synced_at":"2026-09-29T03:00:00Z"}`), 0o644)
	r, err := ReadRevision(dir)
	if err != nil || r == nil || r.Commit == nil || *r.Commit != "abc" {
		t.Fatalf("got %+v %v", r, err)
	}
}

func TestGitHubLatestCaches(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/yundera/mesh-router-template-root/commits/stable" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`{"sha":"abc","commit":{"message":"fix: thing\n\nbody","committer":{"date":"2026-09-29T00:00:00Z"}}}`))
	}))
	defer srv.Close()
	g := NewGitHub()
	g.API = srv.URL
	repo, _ := ParseRepo("")
	for i := 0; i < 3; i++ {
		l, err := g.Latest(context.Background(), repo, false)
		if err != nil || l.Commit != "abc" || l.Message != "fix: thing" {
			t.Fatalf("latest = %+v %v", l, err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (cached)", calls)
	}
	g.Latest(context.Background(), repo, true)
	if calls != 2 {
		t.Fatalf("force did not refetch")
	}
}

func TestReadPins(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "docker-compose.yml")
	os.WriteFile(p, []byte(`name: mesh
services:
  mesh-router-agent:
    image: ghcr.io/yundera/mesh-router-agent:1.1.1
    container_name: mesh-router-agent
  built:
    build: .
`), 0o644)
	pins, err := ReadPins(p)
	if err != nil || len(pins) != 1 || pins[0].Declared != "ghcr.io/yundera/mesh-router-agent:1.1.1" || pins[0].Project != "mesh" {
		t.Fatalf("pins = %+v %v", pins, err)
	}
}
