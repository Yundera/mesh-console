package status

import (
	"testing"
	"time"

	"github.com/yundera/mesh-console/internal/routing"
	"github.com/yundera/mesh-console/internal/selfcheck"
	"github.com/yundera/mesh-console/internal/update"
)

var now = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func healthy() Inputs {
	issued := now.Add(-2 * 24 * time.Hour)
	later := now.Add(5 * 24 * time.Hour) // a 7-day cert, not yet at half-life
	ended := now.Add(-3 * time.Hour)
	return Inputs{
		Now:           now,
		Routing:       &routing.State{Mode: routing.ModeDirect},
		Update:        update.UpToDate,
		Containers:    []Container{{Name: "caddy", State: "running"}, {Name: "agent", State: "running", Health: "healthy"}},
		LastRun:       &selfcheck.Run{Status: selfcheck.RunSuccess, Ended: &ended},
		CertNotBefore: &issued,
		CertNotAfter:  &later,
		Mail:          &Mail{LastStatus: "sent", Sent7d: 4},
	}
}

func TestCompute(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Inputs)
		overall Level
		tile    string
		level   Level
		label   string
		issues  int
	}{
		{"all good", func(*Inputs) {}, OK, Updates, OK, "Up to date", 0},
		{"tunnel is still online", func(in *Inputs) { in.Routing = &routing.State{Mode: routing.ModeTunnel} }, OK, Reachability, OK, "Online", 0},
		{"offline", func(in *Inputs) { in.Routing = &routing.State{Mode: routing.ModeOffline} }, Bad, Reachability, Bad, "Offline", 1},
		{"backend unreachable is unknown, not bad", func(in *Inputs) { in.Routing = nil }, OK, Reachability, Unknown, "Unknown", 1},
		{"update available", func(in *Inputs) { in.Update = update.Outdated }, Warn, Updates, Warn, "Update available", 1},
		{"unknown update state does not alarm", func(in *Inputs) { in.Update = update.Unknown }, OK, Updates, Unknown, "Unknown", 0},
		{"drift wins over outdated", func(in *Inputs) {
			in.Update = update.Outdated
			in.Containers[0].Drift = true
		}, Warn, Updates, Warn, "Update pending", 1},
		{"failed run wins over drift", func(in *Inputs) {
			in.Containers[0].Drift = true
			in.LastRun = &selfcheck.Run{Status: selfcheck.RunFailed, LastLine: now}
		}, Warn, Updates, Warn, "Last update failed", 1},
		{"running update is info", func(in *Inputs) {
			in.UpdateRunning = true
			in.LastRun = &selfcheck.Run{Status: selfcheck.RunFailed, LastLine: now}
		}, OK, Updates, Info, "Updating…", 0},
		{"container stopped", func(in *Inputs) { in.Containers[1].State = "exited" }, Bad, Services, Bad, "1 service stopped", 1},
		{"container unhealthy", func(in *Inputs) { in.Containers[1].Health = "unhealthy" }, Warn, Services, Warn, "1 service unhealthy", 1},
		{"stack unlisted", func(in *Inputs) { in.Containers = nil; in.StackErr = true }, OK, Services, Unknown, "Unknown", 0},
		{"cert at half-life is normal", func(in *Inputs) { d := now.Add(3 * 24 * time.Hour); in.CertNotAfter = &d }, OK, Reachability, OK, "Online", 0},
		{"cert renewal overdue", func(in *Inputs) { d := now.Add(12 * time.Hour); in.CertNotAfter = &d }, Warn, Reachability, Warn, "Online", 1},
		{"no lifetime known falls back to a week", func(in *Inputs) {
			d := now.Add(3 * 24 * time.Hour)
			in.CertNotBefore, in.CertNotAfter = nil, &d
		}, Warn, Reachability, Warn, "Online", 1},
		{"cert expired", func(in *Inputs) { d := now.Add(-time.Hour); in.CertNotAfter = &d }, Bad, Reachability, Bad, "Certificate expired", 1},
		{"old relay has no mail data", func(in *Inputs) { in.Mail = nil }, OK, Email, Unknown, "No data", 0},
		{"nothing sent yet", func(in *Inputs) { in.Mail = &Mail{} }, OK, Email, Unknown, "Nothing sent yet", 0},
		{"last mail failed", func(in *Inputs) { in.Mail = &Mail{LastStatus: "failed"} }, Warn, Email, Warn, "Last email failed", 1},
		{"relay skipping", func(in *Inputs) { in.Mail = &Mail{LastStatus: "skipped"} }, Warn, Email, Warn, "Not delivering", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := healthy()
			c.mutate(&in)
			s := Compute(in)
			if s.Level != c.overall {
				t.Errorf("overall = %s, want %s", s.Level, c.overall)
			}
			tile := s.Tiles[c.tile]
			if tile.Level != c.level || tile.Label != c.label {
				t.Errorf("%s tile = %s %q, want %s %q", c.tile, tile.Level, tile.Label, c.level, c.label)
			}
			if len(s.Issues) != c.issues {
				t.Errorf("issues = %+v, want %d", s.Issues, c.issues)
			}
		})
	}
}

func TestIssuesWorstFirst(t *testing.T) {
	in := healthy()
	in.Update = update.Outdated
	in.Routing = &routing.State{Mode: routing.ModeOffline}
	s := Compute(in)
	if len(s.Issues) != 2 || s.Issues[0].Level != Bad {
		t.Fatalf("issues = %+v", s.Issues)
	}
}
