// Package certs reports which certificate each name of this box is served with,
// who holds its key, and why Let's Encrypt failed when it did.
//
// Phase 1.1 rule (mesh-router-nameserver docs/phase-1.1-gateway-tls.md): one key
// per name.
//   - dash names (<app>-<domain>) end TLS at Cloudflare / the gateway; the box
//     answers the gateway's hop with cert0 (mesh CA, <label>.box.<zone>);
//   - dot names (<app>.<domain>) and the apex end TLS on the box: a Let's Encrypt
//     certificate by DNS-01 once the zone is delegated (zone.go);
//   - sslip.io names: Let's Encrypt by HTTP-01, on the box;
//   - IP names (<ip>.<IP_ZONE>): cert0, for the Worker, not for browsers.
//
// Only names whose certificate the box itself obtains (sslip, dot, apex) are
// probed: a local handshake on a dash or IP name always returns cert0, which is
// not what a visitor sees.
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

// Kind is what a name is, which decides who holds its key.
type Kind string

const (
	KindSslip Kind = "sslip" // <x>.sslip.io: Let's Encrypt by HTTP-01, key on the box
	KindDash  Kind = "dash"  // <app>-<domain>: TLS ends at Cloudflare / the gateway
	KindDot   Kind = "dot"   // <app>.<domain>: the box's DNS-01 wildcard
	KindApex  Kind = "apex"  // <domain>
	KindIP    Kind = "ip"    // under IP_ZONE: cert0 (mesh CA)
	KindOther Kind = "other" // a custom domain pointed at the box
)

// Probed says whether the box's own handshake shows what a browser sees.
func (k Kind) Probed() bool { return k == KindSslip || k == KindDot || k == KindApex }

// KindOf classifies host for the box domain <domainName>.<serverDomain> and the
// IP zone (may be empty). Usernames carry no dash, so a single label under the
// server domain with a dash is a dash name.
func KindOf(host, domainName, serverDomain, ipZone string) Kind {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	domain := strings.ToLower(domainName + "." + serverDomain)
	switch {
	case strings.HasSuffix(h, ".sslip.io") || h == "sslip.io":
		return KindSslip
	case ipZone != "" && strings.HasSuffix(h, "."+strings.ToLower(ipZone)):
		return KindIP
	case domainName != "" && h == domain:
		return KindApex
	case domainName != "" && strings.HasSuffix(h, "."+domain):
		return KindDot
	case serverDomain != "" && strings.HasSuffix(h, "-"+domain):
		label := strings.TrimSuffix(h, "."+strings.ToLower(serverDomain))
		if !strings.Contains(label, ".") {
			return KindDash
		}
	}
	return KindOther
}

type Row struct {
	Domain string `json:"domain"`
	Kind   Kind   `json:"kind"`
	// Sources are the containers whose caddy labels declare the address, or
	// "(root domain)" for the Caddyfile's own sslip.io block.
	Sources   []string   `json:"sources"`
	Status    Status     `json:"status,omitempty"` // empty: not probed (dash, ip, other)
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
// directive value such as caddy_0.redir may name a host without Caddy serving it.
var (
	addressKey = regexp.MustCompile(`^caddy(_[0-9]+)?$`)
	// A host name, optionally behind a scheme, followed by a port, separator or end.
	hostToken = regexp.MustCompile(`(?i)(?:^|[\s,/])(\*?\.?(?:[0-9a-z_-]+\.)+[a-z][0-9a-z-]*)(?::[0-9]+)?`)
)

// Hosts returns the hosts a container's caddy labels declare as site
// addresses. An address value can hold several addresses (space- or
// comma-separated) with a scheme or port, so hosts are extracted rather than
// split. Placeholders ({$X}, {{...}}) and bare ports are skipped.
func Hosts(labels map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	for k, v := range labels {
		if !addressKey.MatchString(k) || strings.Contains(v, "{") {
			continue
		}
		for _, m := range hostToken.FindAllStringSubmatch(v, -1) {
			h := strings.ToLower(m[1])
			if strings.HasPrefix(h, "*") || strings.HasPrefix(h, ".") {
				continue // a wildcard site is not an address to probe
			}
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
	// DNS-01 (zone names) — checked first: their lines also say "challenge".
	reCAA         = regexp.MustCompile(`error:caa|caa record`)
	reACMEDNS     = regexp.MustCompile(`acme-?dns|presenting|present.{0,40}challenge|update.{0,20}txt`)
	rePropagation = regexp.MustCompile(`propagat|waiting for record`)
)

// Explain sets Reason (and ReasonDetail) on a row that is not on a Let's
// Encrypt certificate, from the latest Caddy log line naming its domain.
func Explain(row *Row, logLine string) {
	if row.Status == LetsEncrypt {
		return
	}
	reason, detail := Reason(row.Kind, row.Status == Unreachable, logLine)
	row.Reason, row.ReasonDetail = reason, detail
}

// Reason is the plain-language cause for a name of kind k that is not on a
// Let's Encrypt certificate, and the trimmed log line it was read from.
func Reason(k Kind, unreachable bool, logLine string) (reason, detail string) {
	detail = strings.TrimSpace(logLine)
	if len(detail) > maxDetail {
		detail = detail[:maxDetail] + "…"
	}
	if detail == "" {
		switch {
		case unreachable:
			return "Caddy answered with no certificate for this address. It may be restarting, or the address is not configured.", ""
		case k == KindDot || k == KindApex:
			return "Not on a Let's Encrypt certificate yet, and Caddy's recent logs hold no error for this name.", ""
		}
		return "Served by the fallback authority, and Caddy's recent logs hold no Let's Encrypt error for this address.", ""
	}
	zone := k == KindDot || k == KindApex
	l := strings.ToLower(detail)
	switch {
	case reCAA.MatchString(l):
		reason = "Let's Encrypt refused: the zone's CAA record allows only this box's registered ACME accounts, and Caddy used another one. The box reports its accounts within about 2 minutes and Caddy retries on its own."
	case reRateLimit.MatchString(l):
		reason = "Let's Encrypt rate limit reached. Caddy retries on its own once the limit window passes."
	case zone && reACMEDNS.MatchString(l):
		reason = "Caddy could not publish the DNS-01 challenge through the mesh nameserver API."
	case zone && rePropagation.MatchString(l):
		reason = "The DNS-01 challenge record did not reach the nameservers in time."
	default:
		reason = httpReason(l, zone)
	}
	return reason, detail
}

func httpReason(l string, zone bool) string {
	switch {
	case reDNS.MatchString(l):
		return "The address could not be looked up in DNS."
	case reCanceled.MatchString(l):
		return "The last attempt was interrupted by a Caddy configuration reload. Caddy tries again on its own."
	case reTimeout.MatchString(l):
		return "The connection to Let's Encrypt timed out."
	case reRefused.MatchString(l):
		return "The connection was refused during the Let's Encrypt check."
	case reChallenge.MatchString(l) && zone:
		return "The DNS-01 challenge failed: Let's Encrypt did not find the expected record in the zone."
	case reChallenge.MatchString(l):
		return "Let's Encrypt could not reach this box on ports 80/443 to verify the address."
	}
	return "Let's Encrypt did not issue a certificate. See the Caddy log line for details."
}
