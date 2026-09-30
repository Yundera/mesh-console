package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yundera/mesh-console/internal/certs"
	"github.com/yundera/mesh-console/internal/meshenv"
	"github.com/yundera/mesh-console/internal/probe"
)

// How far back Caddy's log is read for the reason an issuance failed. Caddy
// backs off failed ACME attempts for up to a day, so two weeks always holds
// the latest attempt for an address that is still failing.
const certLogWindow = 14 * 24 * time.Hour

// Handshakes run in parallel, but a box can serve a hundred addresses.
const certProbeWorkers = 8

type gatewayCert struct {
	*probe.Cert
	Renewal certs.Renewal `json:"renewal"`
}

// handleCertificates is on-demand only (the Certificates page): it does one
// handshake per sslip.io address and reads Caddy's log, which is too slow for
// the Overview's poll.
func (s *Server) handleCertificates(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	now := time.Now()
	out := map[string]any{"snapshotAt": now.UTC()}

	// The mesh CA certificate behind the box domain and *.nip.io.
	if c, err := probe.AgentCert(s.cfg.MeshDir); err != nil {
		out["gatewayError"] = err.Error()
	} else {
		out["gateway"] = gatewayCert{Cert: c, Renewal: certs.RenewalOf(c.NotBefore, c.NotAfter, now)}
	}

	if s.docker == nil {
		out["error"] = "docker socket unavailable, so the addresses Caddy serves cannot be listed"
		writeJSON(w, http.StatusOK, out)
		return
	}
	all, err := s.docker.List(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// caddy-docker-proxy serves the labels of running containers only.
	sources := map[string][]string{}
	for _, c := range all {
		if c.State != "running" {
			continue
		}
		for _, h := range certs.SslipHosts(c.Labels) {
			sources[h] = append(sources[h], c.Name)
		}
	}
	ipDash := env.Get("PUBLIC_IP_DASH")
	if ipDash == "" && env.Get("PUBLIC_IP") != "" {
		ipDash = meshenv.Dash(env.Get("PUBLIC_IP"))
	}
	if ipDash != "" {
		root := strings.ToLower(ipDash + ".sslip.io")
		sources[root] = append(sources[root], certs.RootSource)
	}

	domains := make([]string, 0, len(sources))
	for d := range sources {
		domains = append(domains, d)
	}
	sort.Strings(domains)

	rows := make([]certs.Row, len(domains))
	var wg sync.WaitGroup
	sem := make(chan struct{}, certProbeWorkers)
	for i, d := range domains {
		srcs := sources[d]
		sort.Strings(srcs)
		rows[i] = certs.Row{Domain: d, Sources: srcs}
		wg.Add(1)
		go func(row *certs.Row) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c, err := certs.Probe(ctx, s.cfg.CaddyHost, row.Domain)
			certs.Classify(row, c, err, now)
		}(&rows[i])
	}
	wg.Wait()

	// Only read the log when something needs explaining.
	var failing []string
	for _, row := range rows {
		if row.Status != certs.LetsEncrypt {
			failing = append(failing, row.Domain)
		}
	}
	last := certs.LastLines{}
	if len(failing) > 0 {
		if err := s.docker.ScanLogs(ctx, s.cfg.CaddyHost, certLogWindow, func(line string) {
			last.Add(line, failing)
		}); err != nil {
			out["logError"] = "cannot read Caddy's log: " + err.Error()
		}
	}
	for i := range rows {
		certs.Explain(&rows[i], last[rows[i].Domain])
	}

	out["certs"] = rows
	writeJSON(w, http.StatusOK, out)
}
