package hostverb

import (
	"strings"
	"testing"
)

func TestSelfCheck(t *testing.T) {
	v, err := SelfCheck("/DATA/AppData/mesh/scripts", "")
	if err != nil {
		t.Fatal(err)
	}
	if !v.Detached || strings.Join(v.Argv, " ") != "bash /DATA/AppData/mesh/scripts/self-check.sh" {
		t.Fatalf("verb = %+v", v)
	}
	for _, bad := range []string{"", "/", "relative", "/DATA/../etc", "/DATA/$(id)", "/DATA/a b"} {
		if _, err := SelfCheck(bad, ""); err == nil {
			t.Errorf("root %q accepted", bad)
		}
	}
	v, err = SelfCheck("/DATA/AppData/yundera/template/scripts", "")
	if err != nil || v.Argv[1] != "/DATA/AppData/yundera/template/scripts/self-check.sh" {
		t.Fatalf("yundera scripts dir: %+v %v", v, err)
	}
	v, err = SelfCheck("/DATA/AppData/mesh/scripts", "/opt/other/self-check.sh")
	if err != nil || v.Argv[1] != "/opt/other/self-check.sh" {
		t.Fatalf("override: %+v %v", v, err)
	}
	if _, err := SelfCheck("/DATA/AppData/mesh/scripts", "/x;rm -rf /"); err == nil {
		t.Error("bad script accepted")
	}
}

func TestSetDefaultApp(t *testing.T) {
	v, err := SetDefaultApp("/DATA/AppData/yundera/template/scripts", "host.docker.internal", 3000)
	if err != nil {
		t.Fatal(err)
	}
	// The template's own tool; values are separate positional args, never a
	// shell string.
	if strings.Join(v.Argv, "|") != "bash|/DATA/AppData/yundera/template/scripts/tools/set-default-app.sh|host.docker.internal|3000" {
		t.Fatalf("argv = %q", v.Argv)
	}

	for _, host := range []string{"", "-x", "a b", "a;b", "$(id)", "a`id`", "a|b", "a\nb", "../x", strings.Repeat("a", 200)} {
		if _, err := SetDefaultApp("/DATA/AppData/mesh/scripts", host, 80); err == nil {
			t.Errorf("host %q accepted", host)
		}
	}
	for _, port := range []int{0, -1, 65536} {
		if _, err := SetDefaultApp("/DATA/AppData/mesh/scripts", "maison", port); err == nil {
			t.Errorf("port %d accepted", port)
		}
	}
	for _, bad := range []string{"", "/", "relative", "/DATA/../etc", "/DATA/$(id)"} {
		if _, err := SetDefaultApp(bad, "maison", 80); err == nil {
			t.Errorf("scripts dir %q accepted", bad)
		}
	}
}

func TestMigrate(t *testing.T) {
	const scripts = "/DATA/AppData/mesh/scripts"
	v, err := MigrateStart(scripts, "migration@203.0.113.9", "https://orch.example.com/pcs/migration-callback?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(v.Argv, "|") != "bash|/DATA/AppData/mesh/scripts/tools/migrate.sh|start|--to|migration@203.0.113.9|--status-url|https://orch.example.com/pcs/migration-callback?token=abc" {
		t.Fatalf("argv = %q", v.Argv)
	}
	if v.Detached {
		t.Fatal("start must be synchronous: the script detaches itself")
	}
	for _, ok := range []string{"migration@new-box.example.com", "root@2001:db8::1", "u@[2001:db8::1]"} {
		if _, err := MigratePreflight(scripts, ok); err != nil {
			t.Errorf("target %q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "host", "@host", "Root@host", "u@-x", "u@a b", "u@a;b", "u@$(id)", "u@a`id`", "u@h\n", "-oProxyCommand=x@h"} {
		if _, err := MigratePreflight(scripts, bad); err == nil {
			t.Errorf("target %q accepted", bad)
		}
		if _, err := MigrateStart(scripts, bad, ""); err == nil {
			t.Errorf("start target %q accepted", bad)
		}
	}
	for _, bad := range []string{"http://x", "https://a b", "https://x/'y", "https://x/$(id)", "https://x/`id`", "ftp://x"} {
		if _, err := MigrateStart(scripts, "u@h", bad); err == nil {
			t.Errorf("status URL %q accepted", bad)
		}
	}
	if v, err := MigrateStart(scripts, "u@h", ""); err != nil || len(v.Argv) != 5 {
		t.Fatalf("no status URL: %+v %v", v, err)
	}
	for _, f := range []func(string) (Verb, error){MigrateKey, MigrateCancel} {
		if _, err := f("/DATA/$(id)"); err == nil {
			t.Error("bad scripts dir accepted")
		}
	}
}
