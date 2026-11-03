package certs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Let's Encrypt directories as Caddy names them in its storage.
const (
	leProdDir    = "acme-v02.api.letsencrypt.org-directory"
	leStagingDir = "acme-staging-v02.api.letsencrypt.org-directory"
)

// How often mesh-router-caddy re-reports an unchanged account list (daily),
// plus slack for its tick.
const caaReportMaxAge = 26 * time.Hour

// Account is a Let's Encrypt ACME account in Caddy's storage.
type Account struct {
	URI     string `json:"uri"`
	Staging bool   `json:"staging"`
	// Current: the production account of the email Caddy is configured with,
	// the one its next order uses.
	Current  bool `json:"current"`
	Reported bool `json:"reported"`
}

// CAA is the box side of the zone's CAA record: the accounts Caddy holds and
// the list mesh-router-caddy last reported to the backend, which publishes it
// as `CAA issue|issuewild "letsencrypt.org; accounturi=..."`.
type CAA struct {
	Delegated  bool       `json:"delegated"`
	Accounts   []Account  `json:"accounts"`
	Reported   []string   `json:"reported"`
	ReportedAt *time.Time `json:"reportedAt,omitempty"`
	// Issues are plain-language warnings; empty means nothing to fix.
	Issues []string `json:"issues,omitempty"`
	Error  string   `json:"error,omitempty"`
}

type accountFile struct {
	Status   string `json:"status"`
	Location string `json:"location"`
}

// ReadCAA reads Caddy's storage under caddyDataDir (the container's /data).
// Only the account .json files are read, never the key files. email is
// EMAIL in the mesh .env, which Caddy gets as PCS_EMAIL ("" = Caddy's "default" user).
func ReadCAA(caddyDataDir, email string, delegated bool, now time.Time) CAA {
	c := CAA{Delegated: delegated, Accounts: []Account{}, Reported: []string{}}
	acme := filepath.Join(caddyDataDir, "caddy", "acme")
	user := strings.ToLower(strings.TrimSpace(email))
	if user == "" {
		user = "default"
	}
	for _, d := range []struct {
		dir     string
		staging bool
	}{{leProdDir, false}, {leStagingDir, true}} {
		files, _ := filepath.Glob(filepath.Join(acme, d.dir, "users", "*", "*.json"))
		for _, f := range files {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var a accountFile
			if json.Unmarshal(b, &a) != nil || a.Status != "valid" || a.Location == "" {
				continue
			}
			owner := strings.ToLower(filepath.Base(filepath.Dir(f)))
			c.Accounts = append(c.Accounts, Account{URI: a.Location, Staging: d.staging, Current: !d.staging && owner == user})
		}
	}
	sort.Slice(c.Accounts, func(i, j int) bool {
		if c.Accounts[i].Current != c.Accounts[j].Current {
			return c.Accounts[i].Current
		}
		return c.Accounts[i].URI < c.Accounts[j].URI
	})

	statePath := filepath.Join(caddyDataDir, "mesh-caa.state")
	stateOK := false
	if b, err := os.ReadFile(statePath); err == nil {
		var list []string
		if json.Unmarshal(b, &list) == nil {
			c.Reported = list
			stateOK = true
			if fi, err := os.Stat(statePath); err == nil {
				t := fi.ModTime().UTC()
				c.ReportedAt = &t
			}
		} else {
			c.Error = "mesh-caa.state is not a JSON list"
		}
	}
	reported := map[string]bool{}
	for _, u := range c.Reported {
		reported[u] = true
	}
	for i := range c.Accounts {
		c.Accounts[i].Reported = reported[c.Accounts[i].URI]
	}

	if !delegated {
		return c // no CAA is published for a zone that is not delegated
	}
	switch {
	case !stateOK && c.Error == "":
		c.Issues = append(c.Issues, "This box has never reported its ACME accounts: its Caddy image predates CAA reporting, or it has not obtained a certificate yet. Without a report the zone has no CAA record, so any Let's Encrypt account could obtain a certificate for it.")
	case c.ReportedAt != nil && now.Sub(*c.ReportedAt) > caaReportMaxAge:
		c.Issues = append(c.Issues, "The last report is more than a day old, though Caddy re-reports daily: reports are failing. See the [caa] lines in mesh-router-caddy's log.")
	}
	hasCurrent := false
	for _, a := range c.Accounts {
		if a.Current {
			hasCurrent = true
			if stateOK && !a.Reported {
				c.Issues = append(c.Issues, "The account Caddy uses is not in the published CAA record yet, so Let's Encrypt refuses its orders until the next report (within about 2 minutes).")
			}
		}
	}
	if !hasCurrent && len(c.Accounts) > 0 {
		c.Issues = append(c.Issues, "Caddy holds no production account for the configured email yet: it creates one at its next order, which the CAA record refuses until that account is reported (about 2 minutes later, then Caddy retries).")
	}
	return c
}
