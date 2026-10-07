// Package status turns the console's raw evidence (routing, update, containers,
// self-check, mail) into the plain-language verdict the Overview shows.
//
// It does no I/O: the server gathers the inputs, Compute decides. Every rule
// here is a sentence a non-technical owner reads, so keep the wording plain and
// put the technical detail on the Diagnostics page instead.
package status

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yundera/mesh-console/internal/routing"
	"github.com/yundera/mesh-console/internal/selfcheck"
	"github.com/yundera/mesh-console/internal/update"
)

type Level string

const (
	OK      Level = "ok"
	Info    Level = "info" // fine, but something is happening (an update is running)
	Warn    Level = "warn"
	Bad     Level = "bad"
	Unknown Level = "unknown" // no evidence; never makes the headline worse
)

func (l Level) rank() int {
	switch l {
	case Bad:
		return 3
	case Warn:
		return 2
	case OK, Info:
		return 1
	}
	return 0
}

// Tile is one of the Overview's four boxes.
type Tile struct {
	Level  Level  `json:"level"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	// At is an optional timestamp the UI renders as "… ago" after Detail.
	At *time.Time `json:"at,omitempty"`
	// Link is the SPA page with the detail.
	Link string `json:"link"`
}

// Issue is one plain-language thing the owner may want to act on.
type Issue struct {
	Level   Level  `json:"level"`
	Area    string `json:"area"`
	Message string `json:"message"`
	Link    string `json:"link"`
}

type Summary struct {
	Level  Level           `json:"level"`
	Tiles  map[string]Tile `json:"tiles"`
	Issues []Issue         `json:"issues"`
}

// Container is the part of a platform container the verdict needs.
type Container struct {
	Name   string
	State  string // docker state: running, exited, restarting, …
	Health string // healthy, unhealthy, starting, or empty
	Drift  bool   // compose pins a different image than the one running
}

// Mail is the mail relay's recent activity, when the relay reports it.
type Mail struct {
	LastStatus string // sent | skipped | failed | rate_limited; empty when nothing was sent
	LastAt     *time.Time
	Sent7d     int
}

type Inputs struct {
	Now time.Time

	// Routing is nil when the backend could not be asked.
	Routing *routing.State

	Update update.State
	// ManagedBy names the operator driving this box's updates, "" when none.
	ManagedBy string

	// Containers is nil when the Docker socket could not be listed.
	Containers []Container
	StackErr   bool

	// LastRun is the latest self-check run; UpdateRunning is true while one runs.
	LastRun       *selfcheck.Run
	UpdateRunning bool

	// The mesh certificate's validity. It is short-lived (days) and renewed at
	// half-life by mesh-router-agent, so only an overdue renewal is worth a word.
	CertNotBefore, CertNotAfter *time.Time

	// Mail is nil when the relay does not report activity (older relay, or unreachable).
	Mail *Mail
}

const (
	Reachability = "reachability"
	Updates      = "updates"
	Services     = "services"
	Email        = "email"
)

func Compute(in Inputs) Summary {
	if in.Now.IsZero() {
		in.Now = time.Now()
	}
	var issues []Issue
	add := func(l Level, area, link, msg string) {
		issues = append(issues, Issue{Level: l, Area: area, Message: msg, Link: link})
	}

	tiles := map[string]Tile{
		Reachability: reachability(in, add),
		Updates:      updates(in, add),
		Services:     services(in, add),
		Email:        email(in, add),
	}

	overall := Unknown
	for _, t := range tiles {
		if t.Level.rank() > overall.rank() {
			overall = t.Level
		}
	}
	if overall == Info {
		overall = OK
	}
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Level.rank() > issues[j].Level.rank() })
	if issues == nil {
		issues = []Issue{}
	}
	return Summary{Level: overall, Tiles: tiles, Issues: issues}
}

type addFn func(l Level, area, link, msg string)

func reachability(in Inputs, add addFn) Tile {
	const link = "/diagnostics"
	var t Tile
	switch {
	case in.Routing == nil:
		t = Tile{Level: Unknown, Label: "Unknown", Detail: "Could not check right now"}
		add(Warn, Reachability, link, "The console could not ask the network service whether your box is reachable. It will retry shortly.")
	case in.Routing.Mode == routing.ModeOffline:
		t = Tile{Level: Bad, Label: "Offline", Detail: "Not reachable from the internet"}
		add(Bad, Reachability, link, "Your box is not reachable from the internet right now. It reconnects on its own within a few minutes; if this lasts, open Diagnostics.")
	case in.Routing.Mode == routing.ModeDirect:
		t = Tile{Level: OK, Label: "Online", Detail: "Direct connection"}
	case in.Routing.Mode == routing.ModeTunnel:
		t = Tile{Level: OK, Label: "Online", Detail: "Through a secure tunnel"}
	default:
		t = Tile{Level: OK, Label: "Online"}
	}
	if in.CertNotAfter != nil {
		left := in.CertNotAfter.Sub(in.Now)
		overdue := left < 7*24*time.Hour // no lifetime known: fall back to a week
		if in.CertNotBefore != nil {
			overdue = left < in.CertNotAfter.Sub(*in.CertNotBefore)/4
		}
		switch {
		case left < 0:
			add(Bad, Reachability, link, "The security certificate for your addresses has expired, so secure connections to your box fail. Run an update to renew it.")
			t = worse(t, Bad, "Certificate expired")
		case overdue:
			add(Warn, Reachability, link, "The security certificate for your addresses should have renewed by now and expires in "+humanDuration(left)+".")
			t = worse(t, Warn, "")
		}
	}
	t.Link = link
	return t
}

func updates(in Inputs, add addFn) Tile {
	const link = "/update"
	var t Tile
	drift := 0
	for _, c := range in.Containers {
		if c.Drift {
			drift++
		}
	}
	lastFailed := in.LastRun != nil && in.LastRun.Status == selfcheck.RunFailed
	switch {
	case in.UpdateRunning:
		t = Tile{Level: Info, Label: "Updating…", Detail: "An update is running now"}
	case lastFailed:
		t = Tile{Level: Warn, Label: "Last update failed", At: runEnd(in.LastRun)}
		add(Warn, Updates, link, "The last update did not finish cleanly. Open Update to see which step failed and try again.")
	case drift > 0:
		t = Tile{Level: Warn, Label: "Update pending", Detail: plural(drift, "component") + " waiting to restart"}
		add(Warn, Updates, link, "An update was downloaded but is not running yet. It applies at the next update run, or start one now.")
	case in.Update == update.Managed:
		t = Tile{Level: OK, Label: "Managed", Detail: "Updates are managed by " + in.ManagedBy}
	case in.Update == update.Outdated:
		t = Tile{Level: Warn, Label: "Update available"}
		add(Warn, Updates, link, "A newer version is available.")
	case in.Update == update.UpToDate:
		t = Tile{Level: OK, Label: "Up to date"}
		if in.LastRun != nil {
			t.Detail = "Last checked"
			t.At = runEnd(in.LastRun)
		}
	default:
		t = Tile{Level: Unknown, Label: "Unknown", Detail: "Could not check for a newer version"}
	}
	t.Link = link
	return t
}

func services(in Inputs, add addFn) Tile {
	const link = "/diagnostics"
	if in.StackErr {
		return Tile{Level: Unknown, Label: "Unknown", Detail: "Could not list the services", Link: link}
	}
	var down, unhealthy, starting []string
	for _, c := range in.Containers {
		switch {
		case c.State != "running":
			down = append(down, c.Name)
		case c.Health == "unhealthy":
			unhealthy = append(unhealthy, c.Name)
		case c.Health == "starting":
			starting = append(starting, c.Name)
		}
	}
	switch {
	case len(down) > 0:
		add(Bad, Services, link, "Some parts of your box are stopped: "+strings.Join(down, ", ")+".")
		return Tile{Level: Bad, Label: plural(len(down), "service") + " stopped", Link: link}
	case len(unhealthy) > 0:
		add(Warn, Services, link, "Some parts of your box report a problem: "+strings.Join(unhealthy, ", ")+".")
		return Tile{Level: Warn, Label: plural(len(unhealthy), "service") + " unhealthy", Link: link}
	case len(starting) > 0:
		return Tile{Level: Info, Label: "Starting", Detail: plural(len(starting), "service") + " starting up", Link: link}
	case len(in.Containers) == 0:
		return Tile{Level: Unknown, Label: "Unknown", Detail: "No services found", Link: link}
	}
	return Tile{Level: OK, Label: "All running", Detail: plural(len(in.Containers), "service"), Link: link}
}

func email(in Inputs, add addFn) Tile {
	const link = "/email"
	m := in.Mail
	switch {
	case m == nil:
		return Tile{Level: Unknown, Label: "No data", Detail: "Activity not available", Link: link}
	case m.LastStatus == "":
		return Tile{Level: Unknown, Label: "Nothing sent yet", Link: link}
	case m.LastStatus == "failed" || m.LastStatus == "rate_limited":
		add(Warn, Email, link, "The last email an app tried to send was not delivered. Open Email for the reason.")
		return Tile{Level: Warn, Label: "Last email failed", At: m.LastAt, Link: link}
	case m.LastStatus == "skipped":
		add(Warn, Email, link, "Emails from your apps are accepted but not delivered: the mail service is not configured on the server side.")
		return Tile{Level: Warn, Label: "Not delivering", At: m.LastAt, Link: link}
	}
	return Tile{Level: OK, Label: "Working", Detail: fmt.Sprintf("%d sent in 7 days", m.Sent7d), Link: link}
}

// worse lowers a tile to l if it is currently better, optionally relabelling it.
func worse(t Tile, l Level, label string) Tile {
	if l.rank() > t.Level.rank() {
		t.Level = l
		if label != "" {
			t.Label = label
		}
	}
	return t
}

func runEnd(r *selfcheck.Run) *time.Time {
	if r == nil {
		return nil
	}
	if r.Ended != nil {
		return r.Ended
	}
	t := r.LastLine
	return &t
}

func humanDuration(d time.Duration) string {
	if h := int(d.Hours()); h >= 48 {
		return plural(h/24, "day")
	} else if h >= 1 {
		return plural(h, "hour")
	}
	return "less than an hour"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
