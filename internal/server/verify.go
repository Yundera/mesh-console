package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yundera/mesh-console/internal/backend"
	"github.com/yundera/mesh-console/internal/certs"
	"github.com/yundera/mesh-console/internal/meshenv"
)

// handleCertificateVerify backs the "Verify your certificate" page: which
// certificate a browser on ?host= (the page's own address, location.hostname)
// is shown, and the addresses of this console that are end to end, for the
// person to compare with what their browser displays.
//
// host is validated as one of this box's addresses before anything is dialled
// (certs.AddressKind): the dash name is probed from outside like a visitor, the
// names whose key the box holds by a local handshake with Caddy.
func (s *Server) handleCertificateVerify(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	base, domainName, serverDomain, idErr := s.identity(ctx, env)
	domain := ""
	if domainName != "" && serverDomain != "" {
		domain = strings.ToLower(domainName + "." + serverDomain)
	}
	ipDash := env.Get("PUBLIC_IP_DASH")
	if ipDash == "" && env.Get("PUBLIC_IP") != "" {
		ipDash = meshenv.Dash(env.Get("PUBLIC_IP"))
	}
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(r.URL.Query().Get("host")), "."))
	kind, kerr := certs.AddressKind(host, domainName, serverDomain, ipDash)

	out := map[string]any{
		"checkedAt": time.Now().UTC(),
		"domain":    domain,
	}
	if idErr != nil {
		out["backendError"] = idErr.Error()
	}

	// Whether dot names resolve publicly: the zone is delegated to the mesh nameservers.
	var delegated *bool
	if base != "" && domainName != "" {
		if res, err := backend.New(base).Resolve(ctx, domainName); err == nil && res.DNS != nil {
			live := res.DNS.Live
			delegated = &live
		}
	}

	seenOnBox := func(h string, k certs.Kind) certs.Seen {
		c, err := certs.Probe(ctx, s.cfg.CaddyHost, h)
		return certs.SeenOnBox(h, k, c, err)
	}

	var wg sync.WaitGroup
	if kerr == nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if kind == certs.KindDash {
				o, leaf := certs.ProbeOutsideLeaf(ctx, host, kind, serverDomain)
				out["address"] = certs.SeenOutside(o, leaf)
				return
			}
			out["address"] = seenOnBox(host, kind)
		}()
	} else if host != "" {
		out["addressError"] = "This address (" + host + ") is not one of this box's addresses, so it was not checked."
	}

	// This console's end-to-end addresses, as its caddy labels declare them.
	var candidates []string
	if s.docker != nil {
		if all, err := s.docker.List(ctx); err == nil {
			for _, c := range all {
				if c.Name == outsideProbeApp && c.State == "running" {
					candidates = append(candidates, certs.Hosts(c.Labels)...)
				}
			}
		}
	}
	if len(candidates) == 0 && domain != "" {
		candidates = append(candidates, outsideProbeApp+"."+domain)
	}
	var mu sync.Mutex
	var e2e []certs.Seen
	for _, h := range candidates {
		k, err := certs.AddressKind(h, domainName, serverDomain, ipDash)
		if err != nil || k == certs.KindDash || h == host {
			continue
		}
		if (k == certs.KindDot || k == certs.KindApex) && delegated != nil && !*delegated {
			continue // no public DNS for it: a browser cannot open it
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sn := seenOnBox(h, k)
			if !sn.Trusted {
				return // a browser would warn: not an address to send anyone to
			}
			mu.Lock()
			e2e = append(e2e, sn)
			mu.Unlock()
		}()
	}
	wg.Wait()
	// Dot names first (the address to use), then sslip.io.
	rank := map[certs.Kind]int{certs.KindDot: 0, certs.KindApex: 1, certs.KindSslip: 2}
	sort.Slice(e2e, func(i, j int) bool {
		if rank[e2e[i].Kind] != rank[e2e[j].Kind] {
			return rank[e2e[i].Kind] < rank[e2e[j].Kind]
		}
		return e2e[i].Host < e2e[j].Host
	})
	out["endToEnd"] = e2e
	writeJSON(w, http.StatusOK, out)
}
