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
	"strings"
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

// SetUpdateChannelTool is the template's action for its update source, relative
// to its scripts directory: `<stable|dev|local|custom> [url]`, exit 0 saved,
// 2 bad arguments, ExitBusy while a self-check runs. What each channel writes
// (UPDATE_URL, MESH_AUTO_UPDATE) is the template's business.
const SetUpdateChannelTool = "tools/set-update-channel.sh"

// Channels are the names the tool accepts.
var Channels = []string{"stable", "dev", "local", "custom"}

// channelURLRe is the tool's own rule: https or a host file, nothing a shell or
// compose's .env interpolation would treat specially.
var channelURLRe = regexp.MustCompile(`^(https|file)://[A-Za-z0-9._~:/?#@!&()*+,;=%-]+$`)

// SetUpdateChannel switches the template's update source. url is required for
// "custom" and refused for the others.
func SetUpdateChannel(scripts, channel, url string) (Verb, error) {
	if err := checkRoot(scripts); err != nil {
		return Verb{}, err
	}
	argv := []string{"bash", path.Join(scripts, SetUpdateChannelTool), channel}
	switch channel {
	case "stable", "dev", "local":
		if url != "" {
			return Verb{}, fmt.Errorf("channel %q takes no url", channel)
		}
	case "custom":
		if len(url) > 2048 || !channelURLRe.MatchString(url) {
			return Verb{}, errors.New("invalid url: https:// or file://, no spaces or quotes")
		}
		if strings.HasSuffix(url, ".zip") {
			return Verb{}, errors.New("invalid url: the template is a .tar.gz, not a .zip")
		}
		argv = append(argv, url)
	default:
		return Verb{}, fmt.Errorf("unknown channel %q", channel)
	}
	return Verb{Name: "set-update-channel", Argv: argv}, nil
}

var rootRe = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]+$`)

func checkRoot(p string) error {
	if !rootRe.MatchString(p) || path.Clean(p) != p || p == "/" {
		return errors.New("invalid host path (MESH_HOST_ROOT / TEMPLATE_SCRIPTS)")
	}
	return nil
}

// MigrateTool is the template's box-migration script, relative to its scripts
// directory (the mesh template's tools/migrate.sh, which a Yundera PCS runs
// unmodified). The console only drives it; the pipeline, its state
// (data/migrate/status.json) and its log are the template's.
const MigrateTool = "tools/migrate.sh"

// targetRe is user@host as migrate.sh itself validates it: a POSIX user name,
// then a hostname or an IP (IPv6 bare or bracketed).
var targetRe = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}@(\[?[0-9a-fA-F:]+\]?|[a-zA-Z0-9][a-zA-Z0-9.-]{0,252})$`)

// statusURLRe: an https URL with nothing a shell or the script would treat
// specially. Optional - it is where a control plane wants progress pushed.
var statusURLRe = regexp.MustCompile(`^https://[^\s"'` + "`" + `$\\]+$`)

func migrate(scripts, name string, args ...string) (Verb, error) {
	if err := checkRoot(scripts); err != nil {
		return Verb{}, err
	}
	return Verb{Name: name, Argv: append([]string{"bash", path.Join(scripts, MigrateTool)}, args...)}, nil
}

// MigrateKey creates this box's migration keypair if it has none and prints the
// public key - what the target account must accept.
func MigrateKey(scripts string) (Verb, error) {
	return migrate(scripts, "migrate-key", "key")
}

// MigratePreflight checks the target and changes nothing; JSON on stdout.
func MigratePreflight(scripts, target string) (Verb, error) {
	if !targetRe.MatchString(target) {
		return Verb{}, fmt.Errorf("invalid target %q (expected user@host)", target)
	}
	return migrate(scripts, "migrate-preflight", "preflight", "--to", target, "--json")
}

// MigrateStart launches the migration as a systemd unit on the host and returns
// at once (exit 75 when one is already running), so the runner is not held for
// the hours a copy can take.
func MigrateStart(scripts, target, statusURL string) (Verb, error) {
	if !targetRe.MatchString(target) {
		return Verb{}, fmt.Errorf("invalid target %q (expected user@host)", target)
	}
	args := []string{"start", "--to", target}
	if statusURL != "" {
		if len(statusURL) > 2048 || !statusURLRe.MatchString(statusURL) {
			return Verb{}, errors.New("invalid status URL: https only, no spaces or quotes")
		}
		args = append(args, "--status-url", statusURL)
	}
	return migrate(scripts, "migrate-start", args...)
}

// MigrateCancel asks a running migration to stop at its next safe point and
// roll back.
func MigrateCancel(scripts string) (Verb, error) {
	return migrate(scripts, "migrate-cancel", "cancel")
}
