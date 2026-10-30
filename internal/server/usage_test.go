package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yundera/mesh-console/internal/config"
	"github.com/yundera/mesh-console/internal/meshenv"
)

func usageServer(t *testing.T, withTool bool) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	if withTool {
		tool := filepath.Join(dir, "scripts", "tools", "set-usage-report.sh")
		if err := os.MkdirAll(filepath.Dir(tool), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tool, []byte("#!/bin/bash\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{cfg: config.Config{MeshDir: dir, MeshHostRoot: "/DATA/AppData/mesh"}}, dir
}

func TestUsageReportState(t *testing.T) {
	s, dir := usageServer(t, true)
	env := meshenv.Env{"PROVIDER_STR": "https://nsl.sh,user1,SECRETSIG"}

	u := s.usageReportState(env)
	if u.Setting != "on" || !u.Editable || u.Agent != nil {
		t.Fatalf("absent key should read on, editable, no agent state: %+v", u)
	}
	if u.Destination != "nsl.sh" {
		t.Errorf("destination = %q", u.Destination)
	}
	if b, _ := json.Marshal(u); strings.Contains(string(b), "SECRETSIG") || strings.Contains(string(b), "user1") {
		t.Errorf("credential leaked: %s", b)
	}

	state := `{"enabled":true,"days":[{"date":"2026-10-30","apps":["files","nextcloud"],"pending":true}],"lastSentAt":null,"lastError":"HTTP 500","updatedAt":"2026-10-30T12:00:00Z"}`
	if err := os.MkdirAll(filepath.Join(dir, "data", "usage"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data", "usage", "state.json"), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	env["MESH_USAGE_REPORT"] = "off"
	u = s.usageReportState(env)
	if u.Setting != "off" || u.Agent == nil || len(u.Agent.Days) != 1 || u.Agent.Days[0].Apps[1] != "nextcloud" {
		t.Fatalf("state not read: %+v", u)
	}
	if u.Agent.LastError == nil || *u.Agent.LastError != "HTTP 500" {
		t.Errorf("lastError = %v", u.Agent.LastError)
	}
}

func TestUsageReportBlockedWithoutTool(t *testing.T) {
	s, _ := usageServer(t, false)
	u := s.usageReportState(meshenv.Env{"MESH_UPDATES_MANAGED_BY": "Yundera"})
	if u.Editable || u.Blocked == "" {
		t.Fatalf("a template without the tool must not be editable: %+v", u)
	}

	// MESH_UPDATES_MANAGED_BY does not lock it: the opt-out is the owner's.
	s, _ = usageServer(t, true)
	if u := s.usageReportState(meshenv.Env{"MESH_UPDATES_MANAGED_BY": "Yundera"}); !u.Editable {
		t.Fatalf("managed box must stay editable: %+v", u)
	}
}
