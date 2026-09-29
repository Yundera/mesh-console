// Package meshenv reads the mesh stack's .env the way the template's scripts do:
// line by line, never sourced (scripts/library/common.sh).
//
// It is read from disk on every request rather than taken from this container's
// environment. The container env is a snapshot from its last recreate; the file
// is what the self-check and the next `docker compose up` will actually use.
package meshenv

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Env map[string]string

// Load parses ${meshDir}/.env.
func Load(meshDir string) (Env, error) {
	f, err := os.Open(filepath.Join(meshDir, ".env"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	env := Env{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		k, v, ok := ParseLine(sc.Text())
		if ok {
			env[k] = v
		}
	}
	return env, sc.Err()
}

// ParseLine handles KEY=value, optional `export `, surrounding quotes, and
// skips comments and blanks.
func ParseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	k, v, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	k = strings.TrimSpace(k)
	if k == "" {
		return "", "", false
	}
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		v = v[1 : len(v)-1]
	}
	return k, v, true
}

// Get returns the first non-empty value among keys (new name first, then the
// deprecated one — the same fallback the compose file's transition shim uses).
func (e Env) Get(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(e[k]); v != "" {
			return v
		}
	}
	return ""
}

// Provider is the parsed PROVIDER_STR: `<backend_url>,<userid>,<signature>`.
// The signature is deliberately dropped: the console only makes the backend's
// unauthenticated GETs, and never holding it means no handler can leak it.
type Provider struct {
	BackendURL string
	UserID     string
}

func (e Env) Provider() (Provider, error) {
	raw := e.Get("PROVIDER_STR", "PROVIDER")
	if raw == "" {
		return Provider{}, errors.New("PROVIDER_STR is not set")
	}
	parts := strings.Split(raw, ",")
	if len(parts) != 3 {
		return Provider{}, fmt.Errorf("PROVIDER_STR has %d fields, want 3", len(parts))
	}
	p := Provider{
		BackendURL: strings.TrimRight(strings.TrimSpace(parts[0]), "/"),
		UserID:     strings.TrimSpace(parts[1]),
	}
	if p.BackendURL == "" || p.UserID == "" {
		return Provider{}, errors.New("PROVIDER_STR is missing the backend URL or user id")
	}
	return p, nil
}

// APIBase is where mesh-router-backend's router API lives, matching
// ensure-route-registered.sh (`$url/router/api`).
func (p Provider) APIBase() string { return p.BackendURL + "/router/api" }

// Dash turns an IP into the form used in sslip.io / nip.io hostnames, exactly
// as ensure-public-ip.sh derives PUBLIC_IP_DASH.
func Dash(ip string) string {
	return strings.NewReplacer(".", "-", ":", "-").Replace(ip)
}
