package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/yundera/mesh-console/internal/dockerx"
	"github.com/yundera/mesh-console/internal/hostverb"
	"github.com/yundera/mesh-console/internal/selfcheck"
)

// ---- migration -----------------------------------------------------------
//
// The page drives the template's tools/migrate.sh (hostverb.MigrateTool) and
// reads what it leaves in the mesh root: data/migrate/status.json (the steps,
// copy progress and result, written by the script on every change), its log,
// and data/migrate/arrived/ on a box that was migrated onto. The pipeline itself
// runs on the host as a systemd unit, so nothing here waits for it.

func (s *Server) migrateDir() string { return filepath.Join(s.cfg.MeshDir, "data", "migrate") }

// readJSONFile returns the file verbatim when it holds valid JSON, nil otherwise
// (absent, or caught mid-write by an older script).
func readJSONFile(path string) json.RawMessage {
	b, err := os.ReadFile(path)
	if err != nil || !json.Valid(b) {
		return nil
	}
	return json.RawMessage(bytes.TrimSpace(b))
}

// migrationAvailable says whether this box's template ships the migration tool,
// looked for under the /mesh mount the same way as the default-app tool.
func (s *Server) migrationAvailable() (bool, string) {
	rel, err := filepath.Rel(s.cfg.MeshHostRoot, s.cfg.Scripts())
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return true, ""
	}
	if _, err := os.Stat(filepath.Join(s.cfg.MeshDir, rel, hostverb.MigrateTool)); err != nil {
		return false, "this box's template does not ship " + hostverb.MigrateTool + " yet - it arrives with a template update"
	}
	return true, ""
}

func (s *Server) handleMigration(w http.ResponseWriter, r *http.Request) {
	dir := s.migrateDir()
	ok, why := s.migrationAvailable()
	out := map[string]any{"available": ok}
	if !ok {
		out["reason"] = why
	}
	if b, err := os.ReadFile(filepath.Join(dir, "id_ed25519.pub")); err == nil {
		out["key"] = strings.TrimSpace(string(b))
	}
	if st := readJSONFile(filepath.Join(dir, "status.json")); st != nil {
		out["status"] = st
	}
	if st := readJSONFile(filepath.Join(dir, "arrived", "status.json")); st != nil {
		out["arrived"] = st
	}
	if lines, err := selfcheck.TailLines(filepath.Join(dir, "migrate.log"), 200); err == nil {
		out["log"] = lines
	}
	if env, err := s.env(); err == nil {
		if hold := env.Get("MESH_ROUTING_HOLD"); hold != "" {
			out["hold"] = hold
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// migrationVerb runs one of the migrate verbs after the availability check; it
// writes the error response itself and returns nil when the verb did not run.
func (s *Server) migrationVerb(w http.ResponseWriter, r *http.Request, verb hostverb.Verb, err error) *dockerx.RunResult {
	if ok, why := s.migrationAvailable(); !ok {
		writeError(w, http.StatusForbidden, "migration is unavailable: "+why)
		return nil
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil
	}
	res, err := s.runVerb(r.Context(), verb)
	if err != nil {
		s.verbError(w, err)
		return nil
	}
	return res
}

type migrationBody struct {
	Target    string `json:"target"`
	StatusURL string `json:"statusUrl"`
}

func decodeMigrationBody(w http.ResponseWriter, r *http.Request) (migrationBody, bool) {
	var body migrationBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return body, false
	}
	body.Target = strings.TrimSpace(body.Target)
	body.StatusURL = strings.TrimSpace(body.StatusURL)
	return body, true
}

func (s *Server) handleMigrationKey(w http.ResponseWriter, r *http.Request) {
	verb, err := hostverb.MigrateKey(s.cfg.Scripts())
	res := s.migrationVerb(w, r, verb, err)
	if res == nil {
		return
	}
	if res.ExitCode != 0 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "could not create the migration key", "output": res.Output})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": lastLineWith(res.Output, "ssh-")})
}

// handleMigrationPreflight returns the script's JSON verdict. Exit 1 only means
// a check failed, which the verdict says; anything without a verdict is an error.
func (s *Server) handleMigrationPreflight(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeMigrationBody(w, r)
	if !ok {
		return
	}
	verb, err := hostverb.MigratePreflight(s.cfg.Scripts(), body.Target)
	res := s.migrationVerb(w, r, verb, err)
	if res == nil {
		return
	}
	verdict := lastJSONLine(res.Output)
	if verdict == nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "the preflight did not complete", "output": res.Output})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preflight": verdict})
}

func (s *Server) handleMigrationStart(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeMigrationBody(w, r)
	if !ok {
		return
	}
	verb, err := hostverb.MigrateStart(s.cfg.Scripts(), body.Target, body.StatusURL)
	res := s.migrationVerb(w, r, verb, err)
	if res == nil {
		return
	}
	switch res.ExitCode {
	case 0:
		writeJSON(w, http.StatusAccepted, map[string]any{"started": true})
	case hostverb.ExitBusy:
		writeError(w, http.StatusConflict, "a migration is already running on this box")
	default:
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "the migration did not start", "output": res.Output})
	}
}

func (s *Server) handleMigrationCancel(w http.ResponseWriter, r *http.Request) {
	verb, err := hostverb.MigrateCancel(s.cfg.Scripts())
	res := s.migrationVerb(w, r, verb, err)
	if res == nil {
		return
	}
	if res.ExitCode != 0 {
		writeError(w, http.StatusConflict, "no migration is running")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cancelling": true})
}

// The runner merges stdout and stderr (an SSH retry notice can precede the
// verdict), so the verdict is the last line that parses as JSON.
func lastJSONLine(out string) json.RawMessage {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "{") && json.Valid([]byte(l)) {
			return json.RawMessage(l)
		}
	}
	return nil
}

func lastLineWith(out, prefix string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, prefix) {
			return l
		}
	}
	return ""
}
