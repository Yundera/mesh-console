package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/yundera/mesh-console/internal/mail"
	"github.com/yundera/mesh-console/internal/selfcheck"
	"github.com/yundera/mesh-console/internal/status"
	"github.com/yundera/mesh-console/internal/update"
)

// ---- self-check state ----------------------------------------------------

type selfCheckState struct {
	runs       []selfcheck.Run // last 5, oldest first
	parsed     bool
	running    bool // the last run has no completion line and logged recently
	runnerBusy bool
	err        error
}

func (s *Server) selfCheckState(ctx context.Context) selfCheckState {
	var st selfCheckState
	rc, err := selfcheck.Tail(s.logFile(), 2<<20)
	if err != nil {
		st.err = err
	} else {
		runs, perr := selfcheck.Parse(rc, time.Local)
		rc.Close()
		st.err = perr
		if len(runs) > 5 {
			runs = runs[len(runs)-5:]
		}
		st.runs, st.parsed = runs, true
		if len(runs) > 0 {
			last := runs[len(runs)-1]
			st.running = last.Status == selfcheck.RunIncomplete && time.Since(last.LastLine) < stallAfter
		}
	}
	if s.docker != nil {
		st.runnerBusy, _ = s.docker.RunnerBusy(ctx)
	}
	return st
}

// lastFinished is the latest run that reached a verdict. An incomplete or
// interrupted run says nothing about whether the box is up to date.
func (st selfCheckState) lastFinished() *selfcheck.Run {
	for i := len(st.runs) - 1; i >= 0; i-- {
		if r := st.runs[i]; r.Status == selfcheck.RunSuccess || r.Status == selfcheck.RunFailed {
			return &r
		}
	}
	return nil
}

// ---- status (the Overview) -----------------------------------------------

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var (
		wg       sync.WaitGroup
		rr       routingResult
		upState  update.State
		stack    []stackEntry
		stackErr error
		sc       selfCheckState
		stats    *mail.Stats
	)
	wg.Add(5)
	go func() { defer wg.Done(); rr = s.gatherRouting(ctx, env, false) }()
	go func() { defer wg.Done(); _, upState = s.gatherUpdate(ctx, env, false) }()
	go func() { defer wg.Done(); stack, stackErr = s.gatherStack(ctx) }()
	go func() { defer wg.Done(); sc = s.selfCheckState(ctx) }()
	go func() { defer wg.Done(); stats, _ = s.mailStats(ctx) }()
	wg.Wait()

	in := status.Inputs{
		Now:           time.Now(),
		Routing:       rr.state,
		Update:        upState,
		ManagedBy:     env.Get("MESH_UPDATES_MANAGED_BY"),
		StackErr:      stackErr != nil,
		LastRun:       sc.lastFinished(),
		UpdateRunning: sc.running || sc.runnerBusy,
	}
	for _, c := range stack {
		in.Containers = append(in.Containers, status.Container{Name: c.Name, State: c.State, Health: c.Health, Drift: c.Drift})
	}
	if rr.cert != nil {
		nb, na := rr.cert.NotBefore, rr.cert.NotAfter
		in.CertNotBefore, in.CertNotAfter = &nb, &na
	}
	if stats != nil {
		m := &status.Mail{Sent7d: stats.Totals.D7.Sent}
		if len(stats.Recent) > 0 {
			m.LastStatus = stats.Recent[0].Status
			t := stats.Recent[0].Time
			m.LastAt = &t
		}
		in.Mail = m
	}

	out := s.overviewData(ctx, env)
	out["summary"] = status.Compute(in)
	writeJSON(w, http.StatusOK, out)
}

// ---- mail ------------------------------------------------------------------

func (s *Server) mailStats(ctx context.Context) (*mail.Stats, error) {
	if s.docker == nil {
		return nil, errors.New("docker socket unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return mail.ReadStats(ctx, s.docker, s.cfg.MailContainer)
}

func (s *Server) handleMail(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	ctx := r.Context()
	_, domainName, serverDomain, _ := s.identity(ctx, env)
	out := map[string]any{
		"domainName":   domainName,
		"serverDomain": serverDomain,
		"accountEmail": env.Get("EMAIL"),
		"limitPerHour": mail.LimitPerHour,
		"setup":        map[string]any{"host": "smtp", "port": 587, "tls": false, "auth": false},
	}
	if p, err := env.Provider(); err == nil {
		out["relay"] = p.APIBase()
	}
	if stack, err := s.gatherStack(ctx); err == nil {
		for _, c := range stack {
			if c.Name == s.cfg.MailContainer {
				out["relayImage"] = c.Image
				out["relayState"] = c.State
			}
		}
	}
	if st, err := s.mailStats(ctx); err != nil {
		out["statsError"] = err.Error()
	} else {
		out["stats"] = st
	}
	writeJSON(w, http.StatusOK, out)
}

// testMailEvery bounds the test button: the recipient is fixed, but each send
// still counts against the box's hourly relay quota.
const testMailEvery = time.Minute

func (s *Server) handleMailTest(w http.ResponseWriter, r *http.Request) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	// The recipient is always the account email from the mesh .env, never the
	// request: the console must not become a way to mail arbitrary addresses
	// from this box's identity.
	to := env.Get("EMAIL")
	if to == "" {
		writeError(w, http.StatusConflict, "no account email is configured on this box (EMAIL in the mesh .env)")
		return
	}
	s.mailMu.Lock()
	if wait := testMailEvery - time.Since(s.lastTestMail); wait > 0 {
		s.mailMu.Unlock()
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "a test email was just sent; wait a minute before sending another")
		return
	}
	s.lastTestMail = time.Now()
	s.mailMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := s.mailer.Send(ctx, to); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": true, "to": to})
}
