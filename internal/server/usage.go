package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/yundera/mesh-console/internal/hostverb"
	"github.com/yundera/mesh-console/internal/meshenv"
)

// ---- usage reporting -------------------------------------------------------
//
// MESH_USAGE_REPORT as the template applies it (tools/set-usage-report.sh,
// hostverb.SetUsageReportTool), plus what mesh-router-agent collected and sent, from
// its state file data/usage/state.json. The .env is the truth for on/off; the state
// file only says what the agent did with it.

// usageDay is one UTC day the agent keeps: the apps opened, and whether the backend
// has all of them yet.
type usageDay struct {
	Date    string   `json:"date"`
	Apps    []string `json:"apps"`
	Pending bool     `json:"pending"`
}

// usageAgentState is the agent's state file (mesh-router-agent UsageReporter).
type usageAgentState struct {
	Enabled    bool       `json:"enabled"`
	Days       []usageDay `json:"days"`
	LastSentAt *string    `json:"lastSentAt"`
	LastError  *string    `json:"lastError"`
	UpdatedAt  string     `json:"updatedAt"`
}

type usageReport struct {
	// Setting is the owner's choice in .env: on (also when absent) or off.
	Setting string `json:"setting"`
	// Destination is the backend the reports go to (host only, never the credential).
	Destination string `json:"destination,omitempty"`
	// Agent is the agent's state file; absent until an agent that reports has run.
	Agent    *usageAgentState `json:"agent,omitempty"`
	Editable bool             `json:"editable"`
	Blocked  string           `json:"blocked,omitempty"`
}

func (s *Server) usageReportState(env meshenv.Env) usageReport {
	u := usageReport{Setting: "on"}
	if strings.TrimSpace(strings.ToLower(env.Get("MESH_USAGE_REPORT"))) == "off" {
		u.Setting = "off"
	}
	provider := orDefault(env.Get("PROVIDER_STR"), env.Get("PROVIDER"))
	if backend, _, ok := strings.Cut(provider, ","); ok {
		if parsed, err := url.Parse(backend); err == nil && parsed.Host != "" {
			u.Destination = parsed.Host
		}
	}
	if b, err := os.ReadFile(filepath.Join(s.cfg.MeshDir, "data", "usage", "state.json")); err == nil {
		var st usageAgentState
		if json.Unmarshal(b, &st) == nil {
			u.Agent = &st
		}
	}
	// Not locked by MESH_UPDATES_MANAGED_BY: the opt-out is the owner's.
	if s.templateHasTool(hostverb.SetUsageReportTool) {
		u.Editable = true
	} else {
		u.Blocked = "this box's template does not ship " + hostverb.SetUsageReportTool + " yet - it arrives with a template update"
	}
	return u
}

func (s *Server) handleUsageReport(w http.ResponseWriter, _ *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.usageReportState(env))
}

// handleSetUsageReport turns usage reporting on or off through the template.
func (s *Server) handleSetUsageReport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	verb, err := hostverb.SetUsageReport(s.cfg.Scripts(), body.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.templateHasTool(hostverb.SetUsageReportTool) {
		writeError(w, http.StatusForbidden, "this box's template does not ship "+hostverb.SetUsageReportTool+" yet")
		return
	}
	res, err := s.runVerb(r.Context(), verb)
	if err != nil {
		s.verbError(w, err)
		return
	}
	switch res.ExitCode {
	case 0:
	case hostverb.ExitBusy:
		writeError(w, http.StatusConflict, "a self-check is running on this box - try again once it has finished")
		return
	default:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "the change was not applied", "result": res})
		return
	}
	out := map[string]any{"result": res}
	if env, err := s.env(); err == nil {
		out["usageReport"] = s.usageReportState(env)
	}
	writeJSON(w, http.StatusOK, out)
}
