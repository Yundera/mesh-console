// Package hostverb is the complete list of things mesh-console can make the
// host do. There is no generic "run a command" path: every verb is a fixed
// argv built here, user input only ever lands in a validated positional
// argument, and nothing is interpolated into a shell string.
//
// The argv runs inside the host's namespaces (see dockerx.Runner), so paths are
// HOST paths — MeshHostRoot, not the /mesh mount this container reads.
package hostverb

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strconv"
)

type Verb struct {
	Name string
	Argv []string
	// Detached verbs outlive the request (and possibly this container); their
	// progress is read from the self-check log, not from the runner.
	Detached bool
}

// SelfCheck runs the mesh self-check — the same thing the nightly cron runs,
// which syncs the template (when MESH_AUTO_UPDATE allows), pulls and brings the
// stacks up. Its own flock makes a concurrent run exit 0 immediately.
func SelfCheck(hostRoot string) (Verb, error) {
	if err := checkRoot(hostRoot); err != nil {
		return Verb{}, err
	}
	return Verb{
		Name:     "selfcheck",
		Argv:     []string{"bash", path.Join(hostRoot, "scripts", "self-check.sh")},
		Detached: true,
	}, nil
}

// hostRe accepts a container name or a hostname such as host.docker.internal:
// Docker's own name charset, which also rules out every shell metacharacter.
var hostRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// setDefaultAppScript is fixed text; the values arrive as $1..$3. It writes both
// keys through the template's own env-file-manager.sh (atomic, keeps mode and
// owner), then recreates the mesh stack so mesh-router-caddy (root-domain routes
// + catch-all) and auth-registrar (ROOT_CLIENT_ID) pick the new value up —
// both read DEFAULT_SERVICE_HOST, which is exactly why it is one setting.
const setDefaultAppScript = `set -eu
root="$1"; host="$2"; port="$3"
bash "$root/scripts/tools/env-file-manager.sh" set DEFAULT_SERVICE_HOST "$host" "$root/.env"
bash "$root/scripts/tools/env-file-manager.sh" set DEFAULT_SERVICE_PORT "$port" "$root/.env"
cd "$root"
docker compose up -d --remove-orphans
`

// SetDefaultApp points the root domain (and the custom-domain catch-all) at
// host:port.
func SetDefaultApp(hostRoot, host string, port int) (Verb, error) {
	if err := checkRoot(hostRoot); err != nil {
		return Verb{}, err
	}
	if !hostRe.MatchString(host) {
		return Verb{}, fmt.Errorf("invalid host %q", host)
	}
	if port < 1 || port > 65535 {
		return Verb{}, fmt.Errorf("invalid port %d", port)
	}
	return Verb{
		Name: "set-default-app",
		Argv: []string{"bash", "-c", setDefaultAppScript, "set-default-app", hostRoot, host, strconv.Itoa(port)},
	}, nil
}

var rootRe = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]+$`)

func checkRoot(p string) error {
	if !rootRe.MatchString(p) || path.Clean(p) != p || p == "/" {
		return errors.New("invalid MESH_HOST_ROOT")
	}
	return nil
}
