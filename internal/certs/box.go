package certs

import (
	"crypto/x509"
	"strings"
	"time"
)

// Box is the certificate Caddy serves on the box's own domain, as a browser
// reaching the box directly would see it. Today that is the mesh CA cert
// (gateway_tls): only the gateway and the CF worker trust it, so TLS for the
// public is terminated there. A Let's Encrypt wildcard for *.<domain> can only
// have come from a DNS-01 challenge (the nameserver's acme-dns API), and is
// what would let the box serve the public directly.
type Box struct {
	Domain   string     `json:"domain"`
	Kind     BoxKind    `json:"kind"`
	Issuer   string     `json:"issuer,omitempty"`
	DNSNames []string   `json:"dnsNames,omitempty"`
	Wildcard bool       `json:"wildcard"`         // covers *.<domain>
	Method   string     `json:"method,omitempty"` // "dns-01" when that is the only way it can have been issued
	NotAfter *time.Time `json:"notAfter,omitempty"`
	Renewal  Renewal    `json:"renewal,omitempty"`
	Error    string     `json:"error,omitempty"`
}

type BoxKind string

const (
	BoxLetsEncrypt BoxKind = "letsencrypt"
	BoxMeshCA      BoxKind = "mesh-ca"     // issued by the backend's CA, the same issuer as the agent cert
	BoxOther       BoxKind = "other"       // e.g. Caddy's internal CA
	BoxUnreachable BoxKind = "unreachable" // the handshake produced no certificate
)

// ClassifyBox describes the served certificate c (nil when the handshake failed
// with probeErr). meshIssuer is the agent cert's issuer CN, "" when unknown.
func ClassifyBox(domain string, c *x509.Certificate, probeErr error, meshIssuer string, now time.Time) Box {
	b := Box{Domain: domain}
	if c == nil {
		b.Kind = BoxUnreachable
		if probeErr != nil {
			b.Error = probeErr.Error()
		}
		return b
	}
	b.Issuer = issuerName(c)
	b.DNSNames = c.DNSNames
	na := c.NotAfter
	b.NotAfter = &na
	b.Renewal = RenewalOf(c.NotBefore, c.NotAfter, now)
	for _, n := range c.DNSNames {
		if strings.EqualFold(n, "*."+domain) {
			b.Wildcard = true
		}
	}
	switch {
	case letsEncrypt.MatchString(strings.Join(c.Issuer.Organization, " ")) || letsEncrypt.MatchString(c.Issuer.CommonName):
		b.Kind = BoxLetsEncrypt
		if b.Wildcard {
			b.Method = "dns-01" // Let's Encrypt issues wildcards over DNS-01 only
		}
	case meshIssuer != "" && c.Issuer.CommonName == meshIssuer:
		b.Kind = BoxMeshCA
	default:
		b.Kind = BoxOther
	}
	return b
}
