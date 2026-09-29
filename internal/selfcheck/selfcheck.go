// Package selfcheck parses the mesh self-check's log into runs and steps.
//
// There is no status file: the log is the only record (scripts/self-check.sh,
// scripts/library/log.sh). Every line is `[YYYY-MM-DD HH:MM:SS] [LEVEL] message`
// and the markers this package keys on are:
//
//	=== Mesh self-check starting ===            (also "starting (display) ===")
//	=== Self-check starting ===                 (Yundera template-root's wording)
//	=== [ts] name.sh : starting ===
//	=== [ts] name.sh : success (12s) ===
//	=== [ts] name.sh : failed (exit code: 1, 12s) ===
//	=== [ts] name.sh : not found ===            (display mode only)
//	=== Mesh self-check completed successfully ===
//	=== Mesh self-check completed with failures ===
//
// The step lines come from the same log.sh in both templates; only the run
// start/end wording differs, so both are accepted.
package selfcheck

import (
	"bufio"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const tsLayout = "2006-01-02 15:04:05"

type StepStatus string

const (
	StepRunning StepStatus = "running"
	StepSuccess StepStatus = "success"
	StepFailed  StepStatus = "failed"
)

type Step struct {
	Name       string     `json:"name"`
	Status     StepStatus `json:"status"`
	Started    time.Time  `json:"started"`
	DurationS  int        `json:"durationS,omitempty"`
	ExitCode   int        `json:"exitCode,omitempty"`
	LastOutput string     `json:"lastOutput,omitempty"`
}

type RunStatus string

const (
	// RunIncomplete: no completion line yet. Whether it is still going is not
	// decidable from the log alone — the caller combines it with liveness.
	RunIncomplete  RunStatus = "incomplete"
	RunSuccess     RunStatus = "success"
	RunFailed      RunStatus = "failed"
	RunInterrupted RunStatus = "interrupted" // a later run started before this one completed
)

type Run struct {
	Started  time.Time  `json:"started"`
	Ended    *time.Time `json:"ended,omitempty"`
	Status   RunStatus  `json:"status"`
	Steps    []Step     `json:"steps"`
	LastLine time.Time  `json:"lastLine"`
}

var (
	lineRe  = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\] \[([A-Z]+)\] (.*)$`)
	stepRe  = regexp.MustCompile(`^=== \[[^\]]+\] (\S+) : (starting|success \((\d+)s\)|failed \(exit code: (\d+), (\d+)s\)|not found) ===$`)
	startRe = regexp.MustCompile(`^=== (?:Mesh self|Self)-check starting`)
	doneRe  = regexp.MustCompile(`^=== (?:Mesh self|Self)-check completed (successfully|with failures) ===$`)
)

// Parse reads a log (or its tail) and returns runs oldest first. A partial
// first line from a tail read simply fails to match and is skipped.
func Parse(r io.Reader, loc *time.Location) ([]Run, error) {
	if loc == nil {
		loc = time.Local
	}
	var runs []Run
	var cur *Run
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		m := lineRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		ts, err := time.ParseInLocation(tsLayout, m[1], loc)
		if err != nil {
			continue
		}
		level, msg := m[2], m[3]

		if startRe.MatchString(msg) {
			if cur != nil && cur.Status == RunIncomplete {
				cur.Status = RunInterrupted
				for i := range cur.Steps {
					if cur.Steps[i].Status == StepRunning {
						cur.Steps[i].Status = StepFailed
					}
				}
			}
			runs = append(runs, Run{Started: ts, Status: RunIncomplete, LastLine: ts})
			cur = &runs[len(runs)-1]
			continue
		}
		if cur == nil {
			continue // lines from a run whose start was rotated out of the tail
		}
		cur.LastLine = ts

		if d := doneRe.FindStringSubmatch(msg); d != nil {
			end := ts
			cur.Ended = &end
			if d[1] == "successfully" {
				cur.Status = RunSuccess
			} else {
				cur.Status = RunFailed
			}
			continue
		}
		if s := stepRe.FindStringSubmatch(msg); s != nil {
			name := s[1]
			switch {
			case s[2] == "starting":
				cur.Steps = append(cur.Steps, Step{Name: name, Status: StepRunning, Started: ts})
			case strings.HasPrefix(s[2], "success"):
				st := stepFor(cur, name, ts)
				st.Status = StepSuccess
				st.DurationS, _ = strconv.Atoi(s[3])
			case strings.HasPrefix(s[2], "failed"):
				st := stepFor(cur, name, ts)
				st.Status = StepFailed
				st.ExitCode, _ = strconv.Atoi(s[4])
				st.DurationS, _ = strconv.Atoi(s[5])
			default: // not found
				st := stepFor(cur, name, ts)
				st.Status = StepFailed
				st.LastOutput = "script not found"
			}
			continue
		}
		// Keep the last OUTPUT/ERROR line of the step in flight, which is usually
		// the reason it failed.
		if level == "OUTPUT" || level == "ERROR" || level == "WARN" {
			if n := len(cur.Steps); n > 0 && cur.Steps[n-1].Status == StepRunning {
				if t := strings.TrimSpace(msg); t != "" {
					cur.Steps[n-1].LastOutput = truncate(t, 300)
				}
			}
		}
	}
	return runs, sc.Err()
}

// stepFor returns the most recent step with this name, adding one if the
// starting line was not seen (display mode logs it, but a tail may cut it off).
func stepFor(r *Run, name string, ts time.Time) *Step {
	for i := len(r.Steps) - 1; i >= 0; i-- {
		if r.Steps[i].Name == name {
			return &r.Steps[i]
		}
	}
	r.Steps = append(r.Steps, Step{Name: name, Started: ts})
	return &r.Steps[len(r.Steps)-1]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Tail opens path and returns a reader over its last maxBytes. logrotate uses
// copytruncate, so the file keeps its inode and only ever shrinks at rotation.
func Tail(path string, maxBytes int64) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if st.Size() > maxBytes {
		if _, err := f.Seek(st.Size()-maxBytes, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
	}
	return f, nil
}

// TailLines returns the last n raw lines of path, for the log pane.
func TailLines(path string, n int) ([]string, error) {
	rc, err := Tail(path, 256*1024)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var lines []string
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	return lines, sc.Err()
}
