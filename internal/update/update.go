// Package update answers "is this box's mesh template up to date?".
//
// Installed: ensure-template-sync.sh writes template/.revision.json after each
// successful sync ({url, commit, synced_at}). Boxes that synced before that
// change have no marker, which reads as "unknown", never as "outdated".
//
// Latest: the head commit of the branch UPDATE_URL points at, from the GitHub
// API. Only GitHub archive URLs can be resolved; a file:// or custom tarball
// reports "unknown" rather than guessing.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultUpdateURL mirrors mesh_template_url() in scripts/library/common.sh.
const DefaultUpdateURL = "https://github.com/yundera/mesh-router-template-root/archive/refs/heads/stable.tar.gz"

// DevUpdateURL is the main branch: what tools/set-update-channel.sh writes for
// the "dev" channel.
const DevUpdateURL = "https://github.com/yundera/mesh-router-template-root/archive/refs/heads/main.tar.gz"

// Channel names the update source the way the channel picker shows it: "local"
// when downloads are off (whatever UPDATE_URL says), "stable"/"dev" for the two
// published branches, "custom" for anything else.
func Channel(updateURL string, autoUpdate bool) string {
	if !autoUpdate {
		return "local"
	}
	switch u := strings.TrimSpace(updateURL); {
	case u == "" || strings.EqualFold(u, DefaultUpdateURL):
		return "stable"
	case strings.EqualFold(u, DevUpdateURL):
		return "dev"
	}
	return "custom"
}

type Revision struct {
	URL      string  `json:"url"`
	Commit   *string `json:"commit"`
	SyncedAt string  `json:"synced_at"`
}

// ReadRevision reads the marker. A missing file is (nil, nil).
func ReadRevision(meshDir string) (*Revision, error) {
	b, err := os.ReadFile(filepath.Join(meshDir, "template", ".revision.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Revision
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("template/.revision.json: %w", err)
	}
	return &r, nil
}

// TemplateSyncedAt falls back to the template directory's mtime: the sync swaps
// the whole directory in, so its mtime is the last successful sync.
func TemplateSyncedAt(meshDir string) *time.Time {
	st, err := os.Stat(filepath.Join(meshDir, "template"))
	if err != nil {
		return nil
	}
	t := st.ModTime()
	return &t
}

// Repo is a GitHub owner/repo@branch.
type Repo struct {
	Owner, Name, Branch string
}

var (
	// https://github.com/<o>/<r>/archive/refs/heads/<b>.tar.gz (or .zip — Yundera
	// template-root's UPDATE_URL form)
	archiveRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/archive/refs/heads/(.+)\.(?:tar\.gz|zip)$`)
	// https://codeload.github.com/<o>/<r>/tar.gz/refs/heads/<b>
	codeloadRe = regexp.MustCompile(`^https://codeload\.github\.com/([^/]+)/([^/]+)/tar\.gz/(?:refs/heads/)?(.+)$`)
)

// ParseRepo resolves an UPDATE_URL. ok is false for anything that is not a
// GitHub branch tarball.
func ParseRepo(updateURL string) (Repo, bool) {
	u := strings.TrimSpace(updateURL)
	if u == "" {
		u = DefaultUpdateURL
	}
	for _, re := range []*regexp.Regexp{archiveRe, codeloadRe} {
		if m := re.FindStringSubmatch(u); m != nil {
			return Repo{Owner: m[1], Name: m[2], Branch: m[3]}, true
		}
	}
	return Repo{}, false
}

type Latest struct {
	Commit    string    `json:"commit"`
	Date      string    `json:"date,omitempty"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

// GitHub caches the latest commit per repo for an hour: the unauthenticated API
// allows 60 requests/hour per IP, and the page polls.
type GitHub struct {
	HTTP *http.Client
	TTL  time.Duration
	// API is overridable for tests.
	API string

	mu    sync.Mutex
	cache map[Repo]cached
}

type cached struct {
	latest Latest
	err    error
	at     time.Time
}

func NewGitHub() *GitHub {
	return &GitHub{HTTP: &http.Client{Timeout: 8 * time.Second}, TTL: time.Hour, API: "https://api.github.com"}
}

func (g *GitHub) Latest(ctx context.Context, repo Repo, force bool) (Latest, error) {
	g.mu.Lock()
	if g.cache == nil {
		g.cache = map[Repo]cached{}
	}
	c, ok := g.cache[repo]
	g.mu.Unlock()
	// Errors are cached for a shorter time so a transient failure retries soon.
	ttl := g.TTL
	if c.err != nil {
		ttl = 5 * time.Minute
	}
	if ok && !force && time.Since(c.at) < ttl {
		return c.latest, c.err
	}
	l, err := g.fetch(ctx, repo)
	g.mu.Lock()
	g.cache[repo] = cached{latest: l, err: err, at: time.Now()}
	g.mu.Unlock()
	return l, err
}

func (g *GitHub) fetch(ctx context.Context, repo Repo) (Latest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/commits/%s", g.API, repo.Owner, repo.Name, repo.Branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Latest{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return Latest{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Latest{}, fmt.Errorf("github: %s", resp.Status)
	}
	var body struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message   string `json:"message"`
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return Latest{}, err
	}
	msg, _, _ := strings.Cut(body.Commit.Message, "\n")
	return Latest{Commit: body.SHA, Date: body.Commit.Committer.Date, Message: msg, CheckedAt: time.Now()}, nil
}

type State string

const (
	UpToDate State = "up-to-date"
	Outdated State = "outdated"
	Unknown  State = "unknown"
	// Managed: an operator drives this box's version (MESH_UPDATES_MANAGED_BY),
	// so "is there something newer" is not this box's question.
	Managed State = "managed"
)

// pinnedRe is a commit tarball, the form an operator pins UPDATE_URL to:
// https://github.com/<o>/<r>/archive/<sha>.tar.gz
var pinnedRe = regexp.MustCompile(`^https://github\.com/[^/]+/[^/]+/archive/([0-9a-fA-F]{40})\.tar\.gz$`)

// PinnedCommit is the commit an UPDATE_URL pins, or "" when it is not a commit
// tarball (a branch, a file:// test tree, ...).
func PinnedCommit(updateURL string) string {
	if m := pinnedRe.FindStringSubmatch(strings.TrimSpace(updateURL)); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// Compare decides the headline. Unknown whenever either side is missing.
func Compare(installed *Revision, latest *Latest) State {
	if installed == nil || installed.Commit == nil || *installed.Commit == "" || latest == nil || latest.Commit == "" {
		return Unknown
	}
	if strings.EqualFold(*installed.Commit, latest.Commit) {
		return UpToDate
	}
	return Outdated
}

// AutoUpdateEnabled mirrors ensure-template-sync.sh's opt-out values.
func AutoUpdateEnabled(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "disabled", "off", "0":
		return false
	}
	return true
}
