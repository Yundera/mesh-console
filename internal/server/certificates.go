package server

import (
	"context"
	"crypto/x509"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yundera/mesh-console/internal/backend"
	"github.com/yundera/mesh-console/internal/certs"
	"github.com/yundera/mesh-console/internal/meshenv"
	"github.com/yundera/mesh-console/internal/probe"
	"github.com/yundera/mesh-console/internal/routing"
)

// How far back Caddy's log is read for the reason an issuance failed. Caddy
// backs off failed ACME attempts for up to a day, so two weeks always holds
// the latest attempt for an address that is still failing.
const certLogWindow = 14 * 24 * time.Hour

// Handshakes run in parallel, but a box can serve a hundred addresses.
const certProbeWorkers = 8

// The app every box runs, used for the "seen from the internet" handshakes.
const outsideProbeApp = "mesh-console"

// handleCertificates is on-demand only (the Certificates page): it does one
// local handshake per name whose certificate the box obtains itself, two from
// outside, and reads Caddy's log, which is too slow for the Overview's poll.
func (s *Server) handleCertificates(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	now := time.Now()
	ipZone := env.Get("IP_ZONE")
	out := map[string]any{
		"snapshotAt":  now.UTC(),
		"ipZone":      ipZone,
		"sslipRoutes": !strings.EqualFold(env.Get("SSLIP_ROUTES"), "off"),
	}

	// Who the box is, and what the backend's nameservers do for it.
	base, domainName, serverDomain, idErr := s.identity(ctx, env)
	domain := strings.ToLower(env.Get("DOMAIN"))
	if domain == "" && domainName != "" && serverDomain != "" {
		domain = domainName + "." + serverDomain
	}
	out["domain"], out["domainName"], out["serverDomain"] = domain, domainName, serverDomain
	zone := certs.Zone{Domain: domain, Applied: certs.ReadApplied(s.cfg.CaddyData())}
	switch {
	case base != "" && domainName != "":
		res, err := backend.New(base).Resolve(ctx, domainName)
		if err != nil {
			out["backendError"] = err.Error()
			break
		}
		live := false // no dns block = the backend runs no user zones
		if res.DNS != nil {
			live = res.DNS.Live
			zone.Answer = res.DNS.Answer
			// The gateway passes the apex through only while the backend's probe
			// found a trusted certificate naming it (gateway >= 1.14.0).
			trusted := res.DNS.CertNotAfter != nil
			zone.ApexPassthrough = &trusted
		}
		zone.Live = &live
		// Which path the Worker and the gateway take to the box: the second leg
		// of a dash name (Encryption paths on the page).
		out["routing"] = routing.Infer(res.Routes, res.RoutesTTL)
	case idErr != nil:
		out["backendError"] = idErr.Error()
	}

	// cert0.
	meshIssuer := ""
	if c, err := probe.AgentCert(s.cfg.MeshDir); err != nil {
		out["meshCertError"] = err.Error()
	} else {
		meshIssuer = c.Issuer
		out["meshCert"] = certs.NewMeshCert(c.Subject, c.Issuer, c.DNSNames, c.IPs, c.NotBefore, c.NotAfter, domainName, serverDomain, ipZone, now)
	}

	// The names the running containers declare (caddy-docker-proxy serves the
	// labels of running containers only), plus the Caddyfile's root sslip.io site.
	sources := map[string][]string{}
	if s.docker == nil {
		out["error"] = "docker socket unavailable, so the addresses Caddy serves cannot be listed"
	} else if all, err := s.docker.List(ctx); err != nil {
		out["error"] = "cannot list containers: " + err.Error()
	} else {
		for _, c := range all {
			if c.State != "running" {
				continue
			}
			for _, h := range certs.Hosts(c.Labels) {
				sources[h] = append(sources[h], c.Name)
			}
		}
	}
	ipDash := env.Get("PUBLIC_IP_DASH")
	if ipDash == "" && env.Get("PUBLIC_IP") != "" {
		ipDash = meshenv.Dash(env.Get("PUBLIC_IP"))
	}
	out["ipDash"] = ipDash
	if ipDash != "" {
		root := strings.ToLower(ipDash + ".sslip.io")
		sources[root] = append(sources[root], certs.RootSource)
	}
	hosts := make([]string, 0, len(sources))
	for h := range sources {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	rows := make([]certs.Row, len(hosts))
	for i, h := range hosts {
		srcs := sources[h]
		sort.Strings(srcs)
		rows[i] = certs.Row{Domain: h, Sources: srcs, Kind: certs.KindOf(h, domainName, serverDomain, ipZone)}
	}

	// The wildcard is checked on a dot name an app really declares, else on
	// this app's (the *.<domain> site serves every dot name).
	if domain != "" {
		zone.WildcardHost = outsideProbeApp + "." + domain
		for _, row := range rows {
			if row.Kind == certs.KindDot {
				zone.WildcardHost = row.Domain
				break
			}
		}
	}

	// All local handshakes in one pool; the dash-name outside handshake beside them.
	var wg sync.WaitGroup
	sem := make(chan struct{}, certProbeWorkers)
	handshake := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn()
		}()
	}
	// Dot names of a zone that is not delegated are expected to carry no
	// browser-trusted certificate: not probed, not counted as failures.
	delegated := zone.Delegated()
	probes := func(r certs.Row) bool {
		return r.Kind.Probed() && (r.Kind == certs.KindSslip || delegated)
	}
	for i := range rows {
		if !probes(rows[i]) {
			continue
		}
		row := &rows[i]
		handshake(func() {
			c, err := certs.Probe(ctx, s.cfg.CaddyHost, row.Domain)
			certs.Classify(row, c, err, now)
		})
	}
	if domain != "" {
		handshake(func() {
			c, err := certs.Probe(ctx, s.cfg.CaddyHost, domain)
			zone.Apex = certs.ClassifyBox(domain, c, err, meshIssuer, now)
		})
		handshake(func() {
			c, err := certs.Probe(ctx, s.cfg.CaddyHost, zone.WildcardHost)
			zone.Wildcard = classifyWildcard(domain, zone.WildcardHost, c, err, meshIssuer, now)
		})
	}
	var outside []certs.Outside
	var outMu sync.Mutex
	addOutside := func(o certs.Outside) {
		outMu.Lock()
		outside = append(outside, o)
		outMu.Unlock()
	}
	if domainName != "" && serverDomain != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := outsideProbeApp + "-" + domainName + "." + serverDomain
			addOutside(certs.ProbeOutside(ctx, h, certs.KindDash, serverDomain))
		}()
	}
	wg.Wait()

	// Caddy's log, only when something needs explaining.
	var failing []string
	for _, row := range rows {
		if probes(row) && row.Status != certs.LetsEncrypt {
			failing = append(failing, row.Domain)
		}
	}
	zoneFailing := domain != "" && delegated && (zone.Apex.Kind != certs.BoxLetsEncrypt || zone.Wildcard.Kind != certs.BoxLetsEncrypt)
	if zoneFailing {
		failing = append(failing, domain, "*."+domain)
	}
	last := certs.LastLines{}
	if len(failing) > 0 && s.docker != nil {
		if err := s.docker.ScanLogs(ctx, s.cfg.CaddyHost, certLogWindow, func(line string) {
			last.Add(line, failing)
		}); err != nil {
			out["logError"] = "cannot read Caddy's log: " + err.Error()
		}
	}
	for i := range rows {
		if probes(rows[i]) {
			certs.Explain(&rows[i], last[rows[i].Domain])
		}
	}
	if domain != "" {
		zone.Evaluate(last[domain], last["*."+domain])
		out["zone"] = zone
	}

	// The dot name from outside, once the box serves its own certificate.
	if zone.State == certs.ZoneOK || zone.State == certs.ZoneOverdue {
		addOutside(certs.ProbeOutside(ctx, outsideProbeApp+"."+domain, certs.KindDot, serverDomain))
	}
	sort.Slice(outside, func(i, j int) bool { return outside[i].Kind < outside[j].Kind })
	out["outside"] = outside

	// The mesh compose hands Caddy PCS_EMAIL=${EMAIL}: EMAIL is the .env key.
	out["caa"] = certs.ReadCAA(s.cfg.CaddyData(), env.Get("EMAIL", "PCS_EMAIL"), delegated, now)
	out["certs"] = rows
	writeJSON(w, http.StatusOK, out)
}

// classifyWildcard is ClassifyBox for a dot name: the certificate covers it
// through *.<domain>, which is what Wildcard reports.
func classifyWildcard(domain, host string, c *x509.Certificate, err error, meshIssuer string, now time.Time) certs.Box {
	b := certs.ClassifyBox(domain, c, err, meshIssuer, now)
	b.Domain = host
	return b
}
