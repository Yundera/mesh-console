package certs

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSslipHosts(t *testing.T) {
	labels := map[string]string{
		"caddy_0":               "app-1-2-3-4.nip.io",
		"caddy_2":               "app-1-2-3-4.sslip.io",
		"caddy":                 "https://Other-1-2-3-4.sslip.io:443, api.x-1-2-3-4.sslip.io",
		"caddy_2.reverse_proxy": "{{upstreams 80}}",
		"caddy_0.redir":         "https://redirect-1-2-3-4.sslip.io",
		"com.docker.compose":    "x-1-2-3-4.sslip.io",
	}
	got := SslipHosts(labels)
	want := []string{"api.x-1-2-3-4.sslip.io", "app-1-2-3-4.sslip.io", "other-1-2-3-4.sslip.io"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRootHostDoesNotMatchAppHosts(t *testing.T) {
	root := "1-2-3-4.sslip.io"
	l := LastLines{}
	l.Add(`ERROR could not get certificate {"identifier": "app-1-2-3-4.sslip.io"}`, []string{root})
	if _, ok := l[root]; ok {
		t.Fatal("root address picked up an app address's error")
	}
	l.Add(`ERROR could not get certificate {"identifier": "1-2-3-4.sslip.io"}`, []string{root})
	if _, ok := l[root]; !ok {
		t.Fatal("root address's own error was missed")
	}
}

func TestRenewalOf(t *testing.T) {
	nb := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	week := nb.Add(7 * 24 * time.Hour)
	cases := []struct {
		now  time.Time
		want Renewal
	}{
		{nb.Add(4 * 24 * time.Hour), RenewalOK},      // 3 days of 7 left
		{nb.Add(6 * 24 * time.Hour), RenewalOverdue}, // 1 day of 7 left
		{week.Add(time.Minute), RenewalExpired},
	}
	for _, c := range cases {
		if got := RenewalOf(nb, week, c.now); got != c.want {
			t.Errorf("now=%v: got %s, want %s", c.now, got, c.want)
		}
	}
	// 20 days left is fine on a 90-day cert only because it is above a quarter.
	ninety := nb.Add(90 * 24 * time.Hour)
	if got := RenewalOf(nb, ninety, ninety.Add(-25*24*time.Hour)); got != RenewalOK {
		t.Errorf("90d cert, 25d left: got %s", got)
	}
	if got := RenewalOf(nb, ninety, ninety.Add(-20*24*time.Hour)); got != RenewalOverdue {
		t.Errorf("90d cert, 20d left: got %s", got)
	}
}

func TestClassify(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	mk := func(cn string, org ...string) *x509.Certificate {
		return &x509.Certificate{
			Issuer:    pkix.Name{CommonName: cn, Organization: org},
			NotBefore: now.Add(-24 * time.Hour),
			NotAfter:  now.Add(80 * 24 * time.Hour),
		}
	}
	var r Row
	Classify(&r, mk("R11", "Let's Encrypt"), nil, now)
	if r.Status != LetsEncrypt || r.Issuer != "R11 (Let's Encrypt)" || r.Renewal != RenewalOK {
		t.Fatalf("LE: %+v", r)
	}
	r = Row{}
	Classify(&r, mk("Caddy Local Authority - ECC Intermediate"), nil, now)
	if r.Status != Fallback {
		t.Fatalf("internal: %+v", r)
	}
	r = Row{}
	Classify(&r, nil, errors.New("connection refused"), now)
	if r.Status != Unreachable || r.Error != "connection refused" || r.NotAfter != nil {
		t.Fatalf("unreachable: %+v", r)
	}
}

func TestLastLinesAndExplain(t *testing.T) {
	domains := []string{"app-1-2-3-4.sslip.io", "b-1-2-3-4.sslip.io"}
	l := LastLines{}
	l.Add(`INFO obtaining certificate {"identifier": "app-1-2-3-4.sslip.io"}`, domains)
	l.Add("\x1b[31mERROR\x1b[0m\ttls.obtain\tcould not get certificate from issuer {\"identifier\": \"app-1-2-3-4.sslip.io\", \"error\": \"HTTP 429 urn:ietf:params:acme:error:rateLimited\"}", domains)
	l.Add(`ERROR challenge failed {"identifier": "B-1-2-3-4.sslip.io", "problem": "urn:ietf:params:acme:error:unauthorized"}`, domains)
	if len(l) != 2 {
		t.Fatalf("got %v", l)
	}
	if strings.Contains(l[domains[0]], "\x1b") {
		t.Fatalf("ANSI kept: %q", l[domains[0]])
	}

	r := Row{Domain: domains[0], Status: Fallback}
	Explain(&r, l[domains[0]])
	if !strings.Contains(r.Reason, "rate limit") || r.ReasonDetail == "" {
		t.Fatalf("rate limit: %+v", r)
	}
	r = Row{Domain: domains[1], Status: Fallback}
	Explain(&r, l[domains[1]])
	if !strings.Contains(r.Reason, "ports 80/443") {
		t.Fatalf("challenge: %+v", r)
	}
	r = Row{Status: Fallback}
	Explain(&r, `ERROR tls.obtain could not get certificate from issuer {"error": "[x.sslip.io] solving challenges: [x.sslip.io] context canceled"}`)
	if !strings.Contains(r.Reason, "reload") {
		t.Fatalf("canceled: %+v", r)
	}
	r = Row{Status: LetsEncrypt}
	Explain(&r, "ERROR anything")
	if r.Reason != "" {
		t.Fatalf("LE row got a reason: %+v", r)
	}
	r = Row{Status: Unreachable}
	Explain(&r, "")
	if r.Reason == "" || r.ReasonDetail != "" {
		t.Fatalf("no line: %+v", r)
	}
}
