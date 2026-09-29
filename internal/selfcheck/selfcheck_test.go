package selfcheck

import (
	"strings"
	"testing"
	"time"
)

const sample = `ial line cut by the tail read
[2026-09-28 03:00:00] [INFO] === Mesh self-check starting ===
[2026-09-28 03:00:00] [INFO] === [2026-09-28 03:00:00] ensure-scripts-executable.sh : starting ===
[2026-09-28 03:00:01] [SUCCESS] === [2026-09-28 03:00:01] ensure-scripts-executable.sh : success (1s) ===
[2026-09-28 03:00:01] [INFO] === [2026-09-28 03:00:01] ensure-stack-up.sh : starting ===
[2026-09-28 03:00:02] [OUTPUT] Pulling images
[2026-09-28 03:00:40] [OUTPUT] Error response from daemon: toomanyrequests
[2026-09-28 03:00:41] [ERROR] === [2026-09-28 03:00:41] ensure-stack-up.sh : failed (exit code: 1, 40s) ===
[2026-09-28 03:00:41] [INFO] === Mesh self-check completed with failures ===
[2026-09-29 03:00:00] [INFO] === Mesh self-check starting ===
[2026-09-29 03:00:00] [INFO] === [2026-09-29 03:00:00] ensure-template-sync.sh : starting ===
[2026-09-29 03:00:05] [OUTPUT] downloading
[2026-09-29 10:00:00] [INFO] === Mesh self-check starting (display) ===
[2026-09-29 10:00:00] [INFO] === [2026-09-29 10:00:00] ensure-env-valid.sh : starting ===
[2026-09-29 10:00:02] [SUCCESS] === [2026-09-29 10:00:02] ensure-env-valid.sh : success (2s) ===
[2026-09-29 10:00:02] [INFO] === [2026-09-29 10:00:02] ensure-stack-up.sh : starting ===
[2026-09-29 10:00:03] [OUTPUT] Container mesh-router-caddy  Started
`

func TestParse(t *testing.T) {
	runs, err := Parse(strings.NewReader(sample), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(runs))
	}

	failed := runs[0]
	if failed.Status != RunFailed || failed.Ended == nil || len(failed.Steps) != 2 {
		t.Fatalf("run 0 = %+v", failed)
	}
	up := failed.Steps[1]
	if up.Name != "ensure-stack-up.sh" || up.Status != StepFailed || up.ExitCode != 1 || up.DurationS != 40 {
		t.Fatalf("failed step = %+v", up)
	}
	if !strings.Contains(up.LastOutput, "toomanyrequests") {
		t.Fatalf("last output = %q", up.LastOutput)
	}

	interrupted := runs[1]
	if interrupted.Status != RunInterrupted || interrupted.Steps[0].Status != StepFailed {
		t.Fatalf("run 1 = %+v", interrupted)
	}

	current := runs[2]
	if current.Status != RunIncomplete || len(current.Steps) != 2 {
		t.Fatalf("run 2 = %+v", current)
	}
	if current.Steps[0].Status != StepSuccess || current.Steps[1].Status != StepRunning {
		t.Fatalf("run 2 steps = %+v", current.Steps)
	}
	if want := time.Date(2026, 9, 29, 10, 0, 3, 0, time.UTC); !current.LastLine.Equal(want) {
		t.Fatalf("last line = %v", current.LastLine)
	}
}

func TestParseSuccess(t *testing.T) {
	log := `[2026-09-29 03:00:00] [INFO] === Mesh self-check starting ===
[2026-09-29 03:00:00] [ERROR] === [2026-09-29 03:00:00] ensure-foo.sh : not found ===
[2026-09-29 03:00:09] [INFO] === Mesh self-check completed successfully ===
`
	runs, _ := Parse(strings.NewReader(log), time.UTC)
	if len(runs) != 1 || runs[0].Status != RunSuccess {
		t.Fatalf("runs = %+v", runs)
	}
	if s := runs[0].Steps[0]; s.Status != StepFailed || s.LastOutput != "script not found" {
		t.Fatalf("step = %+v", s)
	}
}
