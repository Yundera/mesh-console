package certs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"regexp"
	"strings"
	"time"
)

// Holder is who holds the key of the certificate a visitor is shown.
type Holder string

const (
	HolderCloudflare Holder = "cloudflare" // Cloudflare's edge (dash names before the cutoff)
	HolderGateway    Holder = "gateway"    // the mesh gateway's own *.<zone> (dash names after the cutoff)
	HolderBox        Holder = "box"        // this box's own Let's Encrypt certificate
	HolderUnknown    Holder = "unknown"
)

// Outside is one handshake made like a visitor's: public DNS, port 443,
// verification on. It shows which certificate the internet sees for the name.
type Outside struct {
	Host     string     `json:"host"`
	Kind     Kind       `json:"kind"`
	Holder   Holder     `json:"holder,omitempty"`
	Issuer   string     `json:"issuer,omitempty"`
	NotAfter *time.Time `json:"notAfter,omitempty"`
	// Verified: a browser accepts it (chain and name). The handshake is retried
	// without verification to still show what is served when it is not.
	Verified bool   `json:"verified"`
	Error    string `json:"error,omitempty"`
}

// Cloudflare's edge certificates come from several CAs (Google Trust Services,
// Let's Encrypt, SSL.com) and say so nowhere in the issuer; the leaf is
// recognised by its Cloudflare issuer when there is one, else by its SANs
// (edge certs carry the zone apex + *.zone, a box cert carries only its own names).
var cloudflareIssuer = regexp.MustCompile(`(?i)cloudflare`)

// HolderOf decides who holds the key of leaf, served on host of kind k.
// serverDomain is the mesh zone (nsl.sh).
func HolderOf(leaf *x509.Certificate, k Kind, serverDomain string) Holder {
	if leaf == nil {
		return HolderUnknown
	}
	issuer := leaf.Issuer.CommonName + " " + strings.Join(leaf.Issuer.Organization, " ")
	if cloudflareIssuer.MatchString(issuer) {
		return HolderCloudflare
	}
	zoneWide := false
	for _, n := range leaf.DNSNames {
		if strings.EqualFold(n, "*."+serverDomain) {
			zoneWide = true
		}
	}
	switch {
	case k == KindDash && zoneWide && !letsEncrypt.MatchString(issuer):
		return HolderCloudflare // edge cert from a CA the gateway does not use
	case k == KindDash && zoneWide:
		// Both Cloudflare (Universal SSL) and, after the cutoff, the gateway
		// serve a Let's Encrypt *.<zone>; the edge cert also carries the apex.
		for _, n := range leaf.DNSNames {
			if strings.EqualFold(n, serverDomain) {
				return HolderCloudflare
			}
		}
		return HolderGateway
	case (k == KindDot || k == KindApex) && !zoneWide && letsEncrypt.MatchString(issuer):
		return HolderBox
	}
	return HolderUnknown
}

// ProbeOutside does a visitor's handshake with host:443.
func ProbeOutside(ctx context.Context, host string, k Kind, serverDomain string) Outside {
	o := Outside{Host: host, Kind: k}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	leaf, err := outsideHandshake(ctx, host, false)
	if err == nil {
		o.Verified = true
	} else {
		o.Error = err.Error()
		var verr *tls.CertificateVerificationError
		if !errors.As(err, &verr) {
			return o // nothing was served: DNS, connect or handshake failure
		}
		if leaf, err = outsideHandshake(ctx, host, true); err != nil {
			return o
		}
	}
	o.Issuer = issuerName(leaf)
	na := leaf.NotAfter
	o.NotAfter = &na
	o.Holder = HolderOf(leaf, k, serverDomain)
	return o
}

func outsideHandshake(ctx context.Context, host string, insecure bool) (*x509.Certificate, error) {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 5 * time.Second},
		// insecure only on the retry after a verification failure, to show
		// what was served; Verified stays false then.
		Config: &tls.Config{ServerName: host, InsecureSkipVerify: insecure}, //nolint:gosec // see above
	}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "443"))
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
