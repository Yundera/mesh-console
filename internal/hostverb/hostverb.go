// Package hostverb is the complete list of things mesh-console can make the
// host do. There is no generic "run a command" path: every verb is a fixed
// argv built here, user input only ever lands in a validated positional
// argument, and nothing is interpolated into a shell string.
//
// The argv runs inside the host's namespaces (see dockerx.Runner), so paths are
// HOST paths — MeshHostRoot / TemplateScripts, not the /mesh mount this
// container reads.
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
//
// scripts is the template's scripts directory (config.Scripts); script, when
// set, overrides ${scripts}/self-check.sh. Both come from deployment config,
// never from a request, and are validated anyway.
func SelfCheck(scripts, script string) (Verb, error) {
	if err := checkRoot(scripts); err != nil {
		return Verb{}, err
	}
	if script == "" {
		script = path.Join(scripts, "self-check.sh")
	}
	if err := checkRoot(script); err != nil {
		return Verb{}, errors.New("invalid SELF_CHECK_SCRIPT")
	}
	return Verb{
		Name:     "selfcheck",
		Argv:     []string{"bash", script},
		Detached: true,
	}, nil
}

// hostRe accepts a container name or a hostname such as host.docker.internal:
// Docker's own name charset, which also rules out every shell metacharacter.
var hostRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// SetDefaultAppTool is the template's action for the default app, relative to
// its scripts directory. THE CONTRACT: both templates ship it with the same
// arguments (<host> <port>) and exit codes. Where the setting is stored and what
// has to be recreated for it to take effect (the mesh stack's Caddy and the auth
// stack's registrar, both of which read DEFAULT_SERVICE_HOST) is the template's
// business — the console only knows this path. Before it existed, the console
// wrote the mesh template's .env and ran `compose up` itself, which on a Yundera
// PCS wrote a generated file and recreated the wrong stack.
const SetDefaultAppTool = "tools/set-default-app.sh"

// ExitBusy is the tool's exit code when a self-check holds the lock.
const ExitBusy = 75

// SetDefaultApp points the root domain (and the custom-domain catch-all) at
// host:port, through the template's own tool.
func SetDefaultApp(scripts, host string, port int) (Verb, error) {
	if err := checkRoot(scripts); err != nil {
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
		Argv: []string{"bash", path.Join(scripts, SetDefaultAppTool), host, strconv.Itoa(port)},
	}, nil
}

var rootRe = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]+$`)

func checkRoot(p string) error {
	if !rootRe.MatchString(p) || path.Clean(p) != p || p == "/" {
		return errors.New("invalid host path (MESH_HOST_ROOT / TEMPLATE_SCRIPTS)")
	}
	return nil
}
