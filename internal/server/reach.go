package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/yundera/mesh-console/internal/certs"
	"github.com/yundera/mesh-console/internal/hostverb"
	"github.com/yundera/mesh-console/internal/meshenv"
)

// ---- how the box is reached ----------------------------------------------
//
// Two additions to the routing answer: the owner's routing force (the
// template's tools/set-routing-mode.sh, hostverb.SetRoutingModeTool) and the
// certificate Caddy serves on the box domain.

// routingForce is MESH_ROUTING_FORCE as the template applies it, plus a pending
// revert read from the tool's state file. The .env is the truth: the state file
// only says when a forced tunnel undoes itself.
type routingForce struct {
	Mode     string     `json:"mode"` // auto | tunnel
	RevertAt *time.Time `json:"revertAt,omitempty"`
	Editable bool       `json:"editable"`
	Blocked  string     `json:"blocked,omitempty"`
	Hold     string     `json:"hold,omitempty"`
}

func (s *Server) routingForce(env meshenv.Env, now time.Time) routingForce {
	f := routingForce{Mode: "auto", Hold: env.Get("MESH_ROUTING_HOLD")}
	if env.Get("MESH_ROUTING_FORCE") == "tunnel" {
		f.Mode = "tunnel"
	}
	if b, err := os.ReadFile(filepath.Join(s.cfg.MeshDir, "data", "routing", "force.json")); err == nil {
		var st struct {
			Mode     string     `json:"mode"`
			RevertAt *time.Time `json:"revertAt"`
		}
		if json.Unmarshal(b, &st) == nil && st.Mode == f.Mode && st.RevertAt != nil && st.RevertAt.After(now) {
			f.RevertAt = st.RevertAt
		}
	}
	switch {
	case !s.templateHasTool(hostverb.SetRoutingModeTool):
		f.Blocked = "this box's template does not ship " + hostverb.SetRoutingModeTool + " yet - it arrives with a template update"
	case f.Hold != "":
		f.Blocked = "routing is held by a migration (" + f.Hold + ")"
	default:
		f.Editable = true
	}
	return f
}

type boxCertCache struct {
	at     time.Time
	domain string
	val    certs.Box
}

// boxCert is the certificate Caddy serves for DOMAIN. Cached: the Diagnostics
// page polls every 30 s and the answer changes with a renewal, not by the minute.
func (s *Server) boxCert(ctx context.Context, domain, meshIssuer string) certs.Box {
	s.boxMu.Lock()
	c := s.boxCache
	s.boxMu.Unlock()
	if c != nil && c.domain == domain && time.Since(c.at) < 5*time.Minute {
		return c.val
	}
	served, err := certs.Probe(ctx, s.cfg.CaddyHost, domain)
	b := certs.ClassifyBox(domain, served, err, meshIssuer, time.Now())
	if b.Kind != certs.BoxUnreachable { // retry a failed handshake on the next poll
		s.boxMu.Lock()
		s.boxCache = &boxCertCache{at: time.Now(), domain: domain, val: b}
		s.boxMu.Unlock()
	}
	return b
}

func (s *Server) handleSetRoutingForce(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	if f := s.routingForce(env, time.Now()); !f.Editable {
		writeError(w, http.StatusForbidden, "changing the routing mode is not possible: "+f.Blocked)
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	verb, err := hostverb.SetRoutingMode(s.cfg.Scripts(), body.Mode)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.runVerb(r.Context(), verb)
	if err != nil {
		s.verbError(w, err)
		return
	}
	switch res.ExitCode {
	case hostverb.ExitBusy:
		writeError(w, http.StatusConflict, "a self-check is running on this box - try again once it has finished")
		return
	case hostverb.ExitRoutingHeld:
		writeError(w, http.StatusConflict, "routing is held by a migration - there is no mode to choose")
		return
	}
	out := map[string]any{"result": res}
	if env, err := s.env(); err == nil {
		out["force"] = s.routingForce(env, time.Now())
	}
	code := http.StatusOK
	if res.ExitCode != 0 {
		code = http.StatusBadGateway
	}
	writeJSON(w, code, out)
}
