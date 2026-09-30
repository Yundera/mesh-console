// Package certs reports the certificate Caddy actually serves on each
// *.sslip.io address of this box, and why Let's Encrypt failed when it did.
//
// Only sslip.io addresses are covered: per the template Caddyfile they are the
// ones issued by Let's Encrypt (with Caddy's internal CA as the fallback
// issuer). The box domain and *.nip.io use the mesh CA cert (gateway_tls),
// which is the agent certificate reported separately.
package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	LetsEncrypt Status = "letsencrypt" // served cert issued by Let's Encrypt
	Fallback    Status = "fallback"    // served cert issued by Caddy's internal CA
	Unreachable Status = "unreachable" // the TLS handshake produced no certificate
)

// Renewal says where a certificate stands in its own lifetime. Certificates of
// very different lifetimes are served here (Let's Encrypt 90 days or less, the
// internal CA and the mesh cert 7 days), so it is never a fixed day count.
type Renewal string

const (
	RenewalOK      Renewal = "ok"
	RenewalOverdue Renewal = "overdue" // under a quarter of its lifetime left: renewal should have happened
	RenewalExpired Renewal = "expired"
)

func RenewalOf(notBefore, notAfter, now time.Time) Renewal {
	left := notAfter.Sub(now)
	switch {
	case left <= 0:
		return RenewalExpired
	case notAfter.After(notBefore) && left < notAfter.Sub(notBefore)/4:
		return RenewalOverdue
	}
	return RenewalOK
}

type Row struct {
	Domain string `json:"domain"`
	// Sources are the containers whose caddy labels declare the address, or
	// "(root domain)" for the Caddyfile's own sslip.io block.
	Sources   []string   `json:"sources"`
	Status    Status     `json:"status"`
	Issuer    string     `json:"issuer,omitempty"`
	NotBefore *time.Time `json:"notBefore,omitempty"`
	NotAfter  *time.Time `json:"notAfter,omitempty"`
	Renewal   Renewal    `json:"renewal,omitempty"`
	// Error is the handshake error of an unreachable address.
	Error string `json:"error,omitempty"`
	// Reason is a plain-language cause when the address is not on a Let's
	// Encrypt cert; ReasonDetail the Caddy log line it was read from, if any.
	Reason       string `json:"reason,omitempty"`
	ReasonDetail string `json:"reasonDetail,omitempty"`
}

// RootSource marks the Caddyfile's root-domain sslip.io block.
const RootSource = "(root domain)"

// Only the site-address keys (caddy, caddy_0, caddy_1, …) are read: a
// directive value such as caddy_0.redir may name an sslip.io host without
// Caddy serving it.
var (
	addressKey = regexp.MustCompile(`^caddy(_[0-9]+)?$`)
	sslipHost  = regexp.MustCompile(`(?i)(?:[0-9a-z_-]+\.)+sslip\.io\b`)
)

// SslipHosts returns the sslip.io hosts a container's caddy labels declare.
// An address value can hold several addresses (space- or comma-separated)
// with a scheme or port, so hosts are extracted rather than split.
func SslipHosts(labels map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for k, v := range labels {
		if !addressKey.MatchString(k) {
			continue
		}
		for _, h := range sslipHost.FindAllString(v, -1) {
			h = strings.ToLower(h)
			if !seen[h] {
				seen[h] = true
				out = append(out, h)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Probe does a TLS handshake with Caddy over the pcs network using domain as
// SNI, which returns exactly the certificate Caddy holds for that site.
// Verification is off on purpose: the question is which cert is served, and a
// fallback one is by definition untrusted.
func Probe(ctx context.Context, caddyHost, domain string) (*x509.Certificate, error) {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		Config:    &tls.Config{ServerName: domain, InsecureSkipVerify: true}, //nolint:gosec // see above
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(caddyHost, "443"))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	peers := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(peers) == 0 {
		return nil, errors.New("no certificate presented")
	}
	return peers[0], nil
}

var letsEncrypt = regexp.MustCompile(`(?i)let'?s\s*encrypt`)

// Classify fills a row from the served certificate (nil when the handshake
// failed with probeErr).
func Classify(row *Row, c *x509.Certificate, probeErr error, now time.Time) {
	if c == nil {
		row.Status = Unreachable
		if probeErr != nil {
			row.Error = probeErr.Error()
		}
		return
	}
	row.Issuer = issuerName(c)
	nb, na := c.NotBefore, c.NotAfter
	row.NotBefore, row.NotAfter = &nb, &na
	row.Renewal = RenewalOf(nb, na, now)
	if letsEncrypt.MatchString(strings.Join(c.Issuer.Organization, " ")) || letsEncrypt.MatchString(c.Issuer.CommonName) {
		row.Status = LetsEncrypt
	} else {
		row.Status = Fallback
	}
}

// issuerName is "R11 (Let's Encrypt)" / "Caddy Local Authority - ECC Intermediate".
func issuerName(c *x509.Certificate) string {
	cn := c.Issuer.CommonName
	org := strings.Join(c.Issuer.Organization, ", ")
	switch {
	case cn == "":
		return org
	case org == "" || strings.Contains(cn, org):
		return cn
	}
	return cn + " (" + org + ")"
}

// ---- reasons from Caddy's log ----------------------------------------------

var (
	certWords  = regexp.MustCompile(`(?i)certificat|acme|obtain`)
	errorWords = regexp.MustCompile(`(?i)error|fail|could not|unable|rate.?limit|denied|problem|no such host|timeout`)
)

// IsCertError keeps only the Caddy log lines about a failed issuance.
func IsCertError(line string) bool {
	return certWords.MatchString(line) && errorWords.MatchString(line)
}

// LastLines keeps, per domain, the most recent certificate-error line naming
// it. Feed it every log line in order.
type LastLines map[string]string

func (l LastLines) Add(line string, domains []string) {
	line = ansi.ReplaceAllString(line, "")
	if !IsCertError(line) {
		return
	}
	lower := strings.ToLower(line)
	for _, d := range domains {
		if containsHost(lower, d) {
			l[d] = line
		}
	}
}

// Caddy's console log format colours the level.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// containsHost matches host only as a whole name: the root address
// 1-2-3-4.sslip.io is a suffix of every app-1-2-3-4.sslip.io.
func containsHost(s, host string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], host)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(host)
		if (j == 0 || !hostChar(s[j-1])) && (end == len(s) || !hostChar(s[end])) {
			return true
		}
		i = j + 1
	}
}

func hostChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_'
}

const maxDetail = 600

var (
	reRateLimit = regexp.MustCompile(`rate.?limit|too many certificates|\b429\b`)
	reDNS       = regexp.MustCompile(`no such host|dns problem|nxdomain|could not resolve|name resolution`)
	reTimeout   = regexp.MustCompile(`timeout|timed out|deadline exceeded`)
	reRefused   = regexp.MustCompile(`connection refused`)
	reCanceled  = regexp.MustCompile(`context canceled`)
	reChallenge = regexp.MustCompile(`unauthorized|incorrect validation|challenge|invalid response|not reachable|connection reset`)
)

// Explain sets Reason (and ReasonDetail) on a row that is not on a Let's
// Encrypt certificate, from the latest Caddy log line naming its domain.
func Explain(row *Row, logLine string) {
	if row.Status == LetsEncrypt {
		return
	}
	detail := strings.TrimSpace(logLine)
	if len(detail) > maxDetail {
		detail = detail[:maxDetail] + "…"
	}
	if detail == "" {
		if row.Status == Unreachable {
			row.Reason = "Caddy answered with no certificate for this address. It may be restarting, or the address is not configured."
		} else {
			row.Reason = "Served by the fallback authority, and Caddy's recent logs hold no Let's Encrypt error for this address."
		}
		return
	}
	row.ReasonDetail = detail
	l := strings.ToLower(detail)
	switch {
	case reRateLimit.MatchString(l):
		row.Reason = "Let's Encrypt rate limit reached. Caddy retries on its own once the limit window passes."
	case reDNS.MatchString(l):
		row.Reason = "The address could not be looked up in DNS."
	case reCanceled.MatchString(l):
		row.Reason = "The last attempt was interrupted by a Caddy configuration reload. Caddy tries again on its own."
	case reTimeout.MatchString(l):
		row.Reason = "The connection to Let's Encrypt timed out."
	case reRefused.MatchString(l):
		row.Reason = "The connection was refused during the Let's Encrypt check."
	case reChallenge.MatchString(l):
		row.Reason = "Let's Encrypt could not reach this box on ports 80/443 to verify the address."
	default:
		row.Reason = "Let's Encrypt did not issue a certificate. See the Caddy log line for details."
	}
}
