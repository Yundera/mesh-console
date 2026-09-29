package hostverb

import (
	"strings"
	"testing"
)

func TestSelfCheck(t *testing.T) {
	v, err := SelfCheck("/DATA/AppData/mesh", "")
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
	v, err = SelfCheck("/DATA/AppData/yundera", "/DATA/AppData/yundera/template/scripts/self-check.sh")
	if err != nil || v.Argv[1] != "/DATA/AppData/yundera/template/scripts/self-check.sh" {
		t.Fatalf("override: %+v %v", v, err)
	}
	if _, err := SelfCheck("/DATA/AppData/mesh", "/x;rm -rf /"); err == nil {
		t.Error("bad script accepted")
	}
}

func TestSetDefaultApp(t *testing.T) {
	v, err := SetDefaultApp("/DATA/AppData/mesh", "host.docker.internal", 3000)
	if err != nil {
		t.Fatal(err)
	}
	// The script is fixed; values are positional args after $0.
	n := len(v.Argv)
	if v.Argv[0] != "bash" || v.Argv[1] != "-c" || v.Argv[2] != setDefaultAppScript ||
		v.Argv[n-3] != "/DATA/AppData/mesh" || v.Argv[n-2] != "host.docker.internal" || v.Argv[n-1] != "3000" {
		t.Fatalf("argv = %q", v.Argv)
	}

	for _, host := range []string{"", "-x", "a b", "a;b", "$(id)", "a`id`", "a|b", "a\nb", "../x", strings.Repeat("a", 200)} {
		if _, err := SetDefaultApp("/DATA/AppData/mesh", host, 80); err == nil {
			t.Errorf("host %q accepted", host)
		}
	}
	for _, port := range []int{0, -1, 65536} {
		if _, err := SetDefaultApp("/DATA/AppData/mesh", "maison", port); err == nil {
			t.Errorf("port %d accepted", port)
		}
	}
}
