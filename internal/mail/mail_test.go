package mail

import (
	"context"
	"errors"
	"testing"
)

type fakeExec struct {
	out string
	err error
}

func (f fakeExec) Exec(context.Context, string, []string) (string, error) { return f.out, f.err }

func TestReadStats(t *testing.T) {
	const fixture = `{"version":"1.1.0","totals":{"h24":{"sent":2,"failed":1,"skipped":0,"rateLimited":0},"d7":{"sent":9,"failed":1,"skipped":0,"rateLimited":0}},
"apps":[{"app":"vaultwarden","from":"vaultwarden.alice@nsl.sh","sent":9,"failed":1,"skipped":0,"last":"2026-09-30T10:00:00Z"}],
"recent":[{"time":"2026-09-30T10:00:00Z","app":"vaultwarden","to":"a@b.c","status":"failed","error":"boom"}],"persistent":true}`
	s, err := ReadStats(context.Background(), fakeExec{out: fixture + "\n"}, "smtp")
	if err != nil {
		t.Fatal(err)
	}
	if s.Totals.D7.Sent != 9 || len(s.Apps) != 1 || s.Apps[0].From != "vaultwarden.alice@nsl.sh" || s.Recent[0].Status != "failed" || !s.Persistent {
		t.Fatalf("decoded %+v", s)
	}
}

func TestReadStatsOldRelay(t *testing.T) {
	for _, f := range []fakeExec{
		{err: errors.New("exit 1: RELAY_CREDENTIAL environment variable is required")},
		{out: "2026/09/30 Mail Gateway v1.0.4\n"},
	} {
		if _, err := ReadStats(context.Background(), f, "smtp"); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%+v: err = %v, want ErrUnsupported", f, err)
		}
	}
	_, err := ReadStats(context.Background(), fakeExec{err: errors.New("Error response from daemon: No such container: smtp")}, "smtp")
	if err == nil || errors.Is(err, ErrUnsupported) {
		t.Errorf("missing container: err = %v", err)
	}
}
