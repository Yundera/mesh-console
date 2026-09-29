// Package dockerx is the console's view of the Docker engine: listing the
// platform's containers, reading local evidence from them, and running host
// verbs in a one-shot privileged container.
package dockerx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
)

type Client struct {
	cli *client.Client
}

func New() (*Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &Client{cli: cli}, nil
}

type Container struct {
	Name     string    `json:"name"`
	Project  string    `json:"project,omitempty"`
	Service  string    `json:"service,omitempty"`
	Image    string    `json:"image"`
	State    string    `json:"state"`
	Health   string    `json:"health,omitempty"`
	Status   string    `json:"status"`
	Created  time.Time `json:"created"`
	Networks []string  `json:"networks"`
	// Ports are the container's exposed/published private ports, the candidates
	// for a reverse_proxy upstream.
	Ports []int `json:"ports"`
}

const (
	labelProject = "com.docker.compose.project"
	labelService = "com.docker.compose.service"
)

// List returns every container (running or not), sorted by name.
func (c *Client) List(ctx context.Context) ([]Container, error) {
	raw, err := c.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(raw))
	for _, r := range raw {
		name := ""
		if len(r.Names) > 0 {
			name = strings.TrimPrefix(r.Names[0], "/")
		}
		ct := Container{
			Name:    name,
			Project: r.Labels[labelProject],
			Service: r.Labels[labelService],
			Image:   r.Image,
			State:   r.State,
			Status:  r.Status,
			Created: time.Unix(r.Created, 0),
			Health:  healthFromStatus(r.Status),
		}
		if r.NetworkSettings != nil {
			for n := range r.NetworkSettings.Networks {
				ct.Networks = append(ct.Networks, n)
			}
			sort.Strings(ct.Networks)
		}
		seen := map[int]bool{}
		for _, p := range r.Ports {
			if p.Type == "tcp" && !seen[int(p.PrivatePort)] {
				seen[int(p.PrivatePort)] = true
				ct.Ports = append(ct.Ports, int(p.PrivatePort))
			}
		}
		sort.Ints(ct.Ports)
		out = append(out, ct)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ExposedPorts reads a container's declared ports from its config, which the
// list endpoint omits for a container that is not running.
func (c *Client) ExposedPorts(ctx context.Context, name string) ([]int, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return nil, err
	}
	var ports []int
	if info.Config != nil {
		for p := range info.Config.ExposedPorts {
			if p.Proto() == "tcp" {
				ports = append(ports, p.Int())
			}
		}
	}
	sort.Ints(ports)
	return ports, nil
}

func healthFromStatus(status string) string {
	switch {
	case strings.Contains(status, "(healthy)"):
		return "healthy"
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(status, "(health: starting)"):
		return "starting"
	}
	return ""
}

// ImageOf returns the image reference a container was created from.
func (c *Client) ImageOf(ctx context.Context, name string) (string, error) {
	info, err := c.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "", err
	}
	if info.Config == nil || info.Config.Image == "" {
		return "", fmt.Errorf("%s has no image", name)
	}
	return info.Config.Image, nil
}

// Exec runs argv in a running container and returns stdout.
func (c *Client) Exec(ctx context.Context, name string, argv []string) (string, error) {
	created, err := c.cli.ContainerExecCreate(ctx, name, container.ExecOptions{
		Cmd: argv, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return "", err
	}
	att, err := c.cli.ContainerExecAttach(ctx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", err
	}
	defer att.Close()
	var out, errBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &errBuf, att.Reader); err != nil {
		return "", err
	}
	insp, err := c.cli.ContainerExecInspect(ctx, created.ID)
	if err != nil {
		return "", err
	}
	if insp.ExitCode != 0 {
		return out.String(), fmt.Errorf("exit %d: %s", insp.ExitCode, strings.TrimSpace(errBuf.String()))
	}
	return out.String(), nil
}

// Handshake is one WireGuard peer's last handshake.
type Handshake struct {
	Interface string    `json:"interface"`
	Peer      string    `json:"peer"`
	Last      time.Time `json:"last"`
	// Never is true for a peer that has not completed a handshake (wg prints 0).
	Never bool `json:"never"`
}

// ParseHandshakes parses `wg show all latest-handshakes`:
// "<iface>\t<peer-pubkey>\t<unix-seconds>" per line.
func ParseHandshakes(out string) []Handshake {
	var hs []Handshake
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		sec, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			continue
		}
		h := Handshake{Interface: f[0], Peer: f[1]}
		if sec == 0 {
			h.Never = true
		} else {
			h.Last = time.Unix(sec, 0)
		}
		hs = append(hs, h)
	}
	return hs
}

// ---- runner --------------------------------------------------------------

// RunnerName is fixed so that "is a host verb already running?" is a name
// lookup, and at most one can run at a time.
const RunnerName = "mesh-console-runner"

var ErrBusy = errors.New("another host action is still running")

// RunnerBusy reports whether a runner container is currently running. A
// leftover stopped one (a synchronous verb whose cleanup was cut short) is
// removed so it cannot block the next verb.
func (c *Client) RunnerBusy(ctx context.Context) (bool, error) {
	info, err := c.cli.ContainerInspect(ctx, RunnerName)
	if errdefs.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.State != nil && info.State.Running {
		return true, nil
	}
	_ = c.cli.ContainerRemove(ctx, RunnerName, container.RemoveOptions{Force: true})
	return false, nil
}

// RunResult is a synchronous verb's outcome.
type RunResult struct {
	ExitCode int64  `json:"exitCode"`
	Output   string `json:"output"`
}

// RunOnHost runs argv in the HOST's mount, UTS, IPC, network and PID
// namespaces, from a one-shot container of image.
//
// Why a container and not SSH (settings-center-app's route): a FOSS box has no
// admin user, sshd config or sudoers set up for us, on purpose. This process
// already holds the Docker socket, which is root on the host, so a privileged
// sibling adds no power — it only gives the verb the host's view.
//
// The runner is not in any compose project, so a self-check's
// `up --remove-orphans` never removes it, and it keeps running if this
// console's own container is recreated mid-verb — which the self-check it runs
// may well do.
//
// detached: start and return; the container removes itself when done.
// Otherwise wait, collect output, remove. For a synchronous verb, ctx must NOT
// be the HTTP request's: a client that navigates away would cancel the wait and
// the deferred force-remove would kill the verb half way through.
func (c *Client) RunOnHost(ctx context.Context, image string, argv []string, detached bool) (*RunResult, error) {
	busy, err := c.RunnerBusy(ctx)
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, ErrBusy
	}
	// Entrypoint is set, not cleared: an empty override is dropped by the API
	// (omitempty) and the image's own entrypoint — the console binary — would run
	// with this argv as its arguments.
	created, err := c.cli.ContainerCreate(ctx,
		&container.Config{
			Image:      image,
			Entrypoint: []string{"nsenter"},
			Cmd:        append([]string{"-t", "1", "-m", "-u", "-i", "-n", "-p", "--"}, argv...),
			Labels:     map[string]string{"mesh-console.runner": "true"},
		},
		&container.HostConfig{
			Privileged:  true,
			PidMode:     "host",
			NetworkMode: "none",
			AutoRemove:  detached,
		}, nil, nil, RunnerName)
	if err != nil {
		if errdefs.IsConflict(err) {
			return nil, ErrBusy
		}
		return nil, err
	}
	if !detached {
		defer func() {
			_ = c.cli.ContainerRemove(context.WithoutCancel(ctx), created.ID, container.RemoveOptions{Force: true})
		}()
	}
	if err := c.cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		if detached {
			_ = c.cli.ContainerRemove(context.WithoutCancel(ctx), created.ID, container.RemoveOptions{Force: true})
		}
		return nil, err
	}
	if detached {
		return nil, nil
	}

	statusCh, errCh := c.cli.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	var code int64
	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	case st := <-statusCh:
		code = st.StatusCode
	}
	rc, err := c.cli.ContainerLogs(ctx, created.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return &RunResult{ExitCode: code}, nil
	}
	defer rc.Close()
	var out bytes.Buffer
	_, _ = stdcopy.StdCopy(&out, &out, rc)
	text := out.String()
	if len(text) > 8000 {
		text = "…" + text[len(text)-8000:]
	}
	return &RunResult{ExitCode: code, Output: text}, nil
}
