package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/yundera/mesh-console/internal/backend"
	"github.com/yundera/mesh-console/internal/dockerx"
	"github.com/yundera/mesh-console/internal/hostverb"
	"github.com/yundera/mesh-console/internal/meshenv"
	"github.com/yundera/mesh-console/internal/probe"
	"github.com/yundera/mesh-console/internal/routing"
	"github.com/yundera/mesh-console/internal/selfcheck"
	"github.com/yundera/mesh-console/internal/update"
)

// platformProjects are the compose projects the console reports on: the mesh
// stack and the auxiliary stacks the template deploys beside it.
var platformProjects = []string{"mesh", "maison", "mesh-console"}

// ---- overview ------------------------------------------------------------

type links struct {
	Root      string `json:"root"`
	Maison    string `json:"maison"`
	Dashboard string `json:"dashboard,omitempty"`
	Sslip     string `json:"sslip,omitempty"`
	Nip       string `json:"nip,omitempty"`
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	domain := env.Get("DOMAIN")
	ipDash := env.Get("PUBLIC_IP_DASH")
	if ipDash == "" && env.Get("PUBLIC_IP") != "" {
		ipDash = meshenv.Dash(env.Get("PUBLIC_IP"))
	}
	_, domainName, serverDomain, idErr := s.identity(r.Context(), env)

	l := links{Root: "https://" + domain, Maison: "https://maison-" + domain}
	if serverDomain != "" {
		l.Dashboard = "https://" + serverDomain + "/dashboard"
	}
	if ipDash != "" {
		l.Sslip = "https://" + ipDash + ".sslip.io"
		l.Nip = "https://" + ipDash + ".nip.io"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"domain":       domain,
		"domainName":   domainName,
		"serverDomain": serverDomain,
		"publicIp":     env.Get("PUBLIC_IP"),
		"publicIpv4":   env.Get("PUBLIC_IPV4"),
		"publicIpv6":   env.Get("PUBLIC_IPV6"),
		"ipDash":       ipDash,
		"email":        env.Get("EMAIL"),
		"links":        l,
		"backendError": errString(idErr),
	})
}

// ---- routing -------------------------------------------------------------

type tunnelEvidence struct {
	Handshakes []dockerx.Handshake `json:"handshakes"`
	Error      string              `json:"error,omitempty"`
}

func (s *Server) handleRouting(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	out := map[string]any{}
	base, domainName, _, idErr := s.identity(ctx, env)
	var res backend.Resolution
	var resErr error
	if base != "" && domainName != "" {
		res, resErr = backend.New(base).Resolve(ctx, domainName)
	} else {
		resErr = idErr
	}
	if resErr != nil {
		out["backendError"] = resErr.Error()
		out["state"] = nil
	} else {
		st := routing.Infer(res.Routes, res.RoutesTTL)
		out["state"] = st
		out["routes"] = res.Routes
		out["routesTtl"] = res.RoutesTTL
		out["lastSeenOnline"] = res.LastSeenOnline
	}

	// Local evidence, gathered even when the backend is unreachable — that is
	// exactly when it matters.
	if s.docker != nil {
		var te tunnelEvidence
		if o, err := s.docker.Exec(ctx, s.cfg.TunnelContainer, []string{"wg", "show", "all", "latest-handshakes"}); err != nil {
			te.Error = err.Error()
		} else {
			te.Handshakes = dockerx.ParseHandshakes(o)
		}
		out["tunnel"] = te
	}
	if c, err := probe.AgentCert(s.cfg.MeshDir); err != nil {
		out["certError"] = err.Error()
	} else {
		out["cert"] = c
	}
	if d := env.Get("DOMAIN"); d != "" {
		out["rootDomain"] = probe.Root(ctx, s.cfg.CaddyHost, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- stack ---------------------------------------------------------------

type stackEntry struct {
	dockerx.Container
	// Declared is the image the stack's compose file pins. When it differs from
	// Image the stack was updated on disk but not brought up yet.
	Declared string `json:"declared,omitempty"`
	Drift    bool   `json:"drift"`
}

func (s *Server) handleStack(w http.ResponseWriter, r *http.Request) {
	if s.docker == nil {
		writeError(w, http.StatusServiceUnavailable, "docker socket unavailable")
		return
	}
	all, err := s.docker.List(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	declared := map[string]string{} // container name -> image
	for _, pin := range s.pins() {
		if pin.Container != "" {
			declared[pin.Container] = pin.Declared
		}
	}
	var out []stackEntry
	for _, c := range all {
		if !contains(platformProjects, c.Project) {
			continue
		}
		e := stackEntry{Container: c, Declared: declared[c.Name]}
		e.Drift = e.Declared != "" && e.Declared != c.Image
		out = append(out, e)
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": out})
}

// pins reads the compose files of the mesh stack and its auxiliary stacks.
// The auxiliary ones live beside the mesh root (${DATA_ROOT}/AppData/<name>),
// which this container does not mount, so only the mesh stack's file is
// readable; the others simply contribute nothing.
func (s *Server) pins() []update.Pin {
	pins, _ := update.ReadPins(filepath.Join(s.cfg.MeshDir, "docker-compose.yml"))
	return pins
}

// ---- update --------------------------------------------------------------

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	updateURL := env.Get("UPDATE_URL", "MESH_TEMPLATE_URL")
	out := map[string]any{
		"updateUrl":   updateURL,
		"autoUpdate":  update.AutoUpdateEnabled(env.Get("MESH_AUTO_UPDATE")),
		"cron":        env.Get("SELF_CHECK_CRON"),
		"windowsMode": env.Get("MESH_WINDOWS_MODE") == "true",
	}
	installed, err := update.ReadRevision(s.cfg.MeshDir)
	if err != nil {
		out["installedError"] = err.Error()
	}
	out["installed"] = installed
	out["templateSyncedAt"] = update.TemplateSyncedAt(s.cfg.MeshDir)

	var latest *update.Latest
	if repo, ok := update.ParseRepo(updateURL); ok {
		out["repo"] = repo.Owner + "/" + repo.Name + "@" + repo.Branch
		l, err := s.github.Latest(r.Context(), repo, r.URL.Query().Get("refresh") == "1")
		if err != nil {
			out["latestError"] = err.Error()
		} else {
			latest = &l
		}
	} else {
		out["latestError"] = "update source is not a GitHub branch tarball; cannot check for a newer version"
	}
	out["latest"] = latest
	out["state"] = update.Compare(installed, latest)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateRun(w http.ResponseWriter, r *http.Request) {
	verb, err := hostverb.SelfCheck(s.cfg.MeshHostRoot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := s.runVerb(r.Context(), verb); err != nil {
		s.verbError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
}

// ---- self-check progress -------------------------------------------------

// A run with no completion line counts as still going if its last log line is
// this recent. The longest steps (image pulls with backoff) log as they go.
const stallAfter = 15 * time.Minute

func (s *Server) handleSelfCheck(w http.ResponseWriter, r *http.Request) {
	logPath := filepath.Join(s.cfg.MeshDir, "log", "mesh.log")
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 || n > 1000 {
		n = 200
	}
	out := map[string]any{}

	rc, err := selfcheck.Tail(logPath, 2<<20)
	if err != nil {
		out["error"] = err.Error()
	} else {
		runs, perr := selfcheck.Parse(rc, time.Local)
		rc.Close()
		if perr != nil {
			out["error"] = perr.Error()
		}
		if len(runs) > 5 {
			runs = runs[len(runs)-5:]
		}
		out["runs"] = runs
		if len(runs) > 0 {
			last := runs[len(runs)-1]
			out["running"] = last.Status == selfcheck.RunIncomplete && time.Since(last.LastLine) < stallAfter
		}
	}
	if s.docker != nil {
		busy, _ := s.docker.RunnerBusy(r.Context())
		out["runnerBusy"] = busy
		if busy {
			out["running"] = true
		}
	}
	if lines, err := selfcheck.TailLines(logPath, n); err == nil {
		out["log"] = lines
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- domain --------------------------------------------------------------

type candidate struct {
	Name    string `json:"name"`
	Project string `json:"project,omitempty"`
	State   string `json:"state"`
	Ports   []int  `json:"ports"`
	OnPcs   bool   `json:"onPcs"`
}

func (s *Server) handleDomain(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	appNet := env.Get("APP_NET")
	if appNet == "" {
		appNet = "pcs"
	}
	out := map[string]any{
		"domain":      env.Get("DOMAIN"),
		"ipDash":      env.Get("PUBLIC_IP_DASH"),
		"publicIp":    env.Get("PUBLIC_IP"),
		"defaultHost": orDefault(env.Get("DEFAULT_SERVICE_HOST"), "maison"),
		"defaultPort": orDefault(env.Get("DEFAULT_SERVICE_PORT"), "80"),
		"network":     appNet,
	}
	if s.docker != nil {
		all, err := s.docker.List(r.Context())
		if err != nil {
			out["candidatesError"] = err.Error()
		} else {
			var cands []candidate
			for _, c := range all {
				// The console, its gate and the runner are never a sensible root app.
				if c.Project == "mesh-console" || c.Name == dockerx.RunnerName {
					continue
				}
				ports := c.Ports
				if len(ports) == 0 {
					ports, _ = s.docker.ExposedPorts(r.Context(), c.Name)
				}
				cands = append(cands, candidate{
					Name: c.Name, Project: c.Project, State: c.State, Ports: ports, OnPcs: contains(c.Networks, appNet),
				})
			}
			// Reachable ones first: Caddy resolves the target by Docker DNS on the
			// pcs network, so anything else 502s.
			sort.SliceStable(cands, func(i, j int) bool { return cands[i].OnPcs && !cands[j].OnPcs })
			out["candidates"] = cands
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSetDefaultApp(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	verb, err := hostverb.SetDefaultApp(s.cfg.MeshHostRoot, body.Host, body.Port)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.runVerb(r.Context(), verb)
	if err != nil {
		s.verbError(w, err)
		return
	}
	out := map[string]any{"result": res}
	if res.ExitCode == 0 {
		if env, err := s.env(); err == nil && env.Get("DOMAIN") != "" {
			// Caddy was just recreated; give it a moment to bind before probing.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out["rootDomain"] = s.probeRootWithRetry(ctx, env.Get("DOMAIN"))
		}
	}
	code := http.StatusOK
	if res.ExitCode != 0 {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, out)
}

func (s *Server) probeRootWithRetry(ctx context.Context, domain string) probe.RootDomain {
	var last probe.RootDomain
	for i := 0; i < 5; i++ {
		last = probe.Root(ctx, s.cfg.CaddyHost, domain)
		if last.Error == "" && last.Status < 500 {
			return last
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(3 * time.Second):
		}
	}
	return last
}

// ---- verbs ---------------------------------------------------------------

// runVerb runs a host verb from this container's own image. A synchronous verb
// runs on a context detached from the request (see dockerx.RunOnHost), bounded
// so a hung `docker compose` cannot pin the runner forever.
func (s *Server) runVerb(reqCtx context.Context, v hostverb.Verb) (*dockerx.RunResult, error) {
	if s.docker == nil {
		return nil, errors.New("docker socket unavailable")
	}
	image := s.cfg.RunnerImage
	if image == "" {
		img, err := s.docker.ImageOf(reqCtx, s.cfg.SelfContainer)
		if err != nil {
			return nil, errors.New("cannot determine runner image (set RUNNER_IMAGE): " + err.Error())
		}
		image = img
	}
	if v.Detached {
		return s.docker.RunOnHost(reqCtx, image, v.Argv, true)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return s.docker.RunOnHost(ctx, image, v.Argv, false)
}

func (s *Server) verbError(w http.ResponseWriter, err error) {
	if errors.Is(err, dockerx.ErrBusy) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
