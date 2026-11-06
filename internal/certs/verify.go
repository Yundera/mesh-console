package certs

import (
	"crypto/x509"
	"errors"
	"strings"
	"time"
)

// Seen is the certificate a browser shows on one address of this box, laid out
// as the browser's certificate viewer lays it out, so a person can compare the
// two (the "Verify your certificate" page).
type Seen struct {
	Host string `json:"host"`
	Kind Kind   `json:"kind"`
	// Holder is who holds the key; EndToEnd is Holder == box: only the box can
	// decrypt the connection.
	Holder   Holder `json:"holder"`
	EndToEnd bool   `json:"endToEnd"`
	// Trusted: a browser accepts the certificate without a warning.
	Trusted     bool       `json:"trusted"`
	IssuedTo    string     `json:"issuedTo,omitempty"` // the subject CN, else the first DNS name
	DNSNames    []string   `json:"dnsNames,omitempty"`
	Issuer      string     `json:"issuer,omitempty"`
	NotBefore   *time.Time `json:"notBefore,omitempty"`
	NotAfter    *time.Time `json:"notAfter,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// ErrNotThisBox: the address is not one this box answers on, so it is not probed
// (the page asks the server to dial the name the browser is on).
var ErrNotThisBox = errors.New("not an address of this box")

// AddressKind validates host as a browser address of this box and returns its
// kind. Only dash, dot and apex names of the box domain and the box's own
// sslip.io names are accepted: anything else would make the console dial an
// arbitrary host. ipDash is the box's public IP, dashed ("" when unknown).
func AddressKind(host, domainName, serverDomain, ipDash string) (Kind, error) {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" || strings.ContainsAny(h, ":/ @?#") || domainName == "" && ipDash == "" {
		return "", ErrNotThisBox
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return "", ErrNotThisBox
		}
		for i := 0; i < len(label); i++ {
			if !hostChar(label[i]) || label[i] == '.' {
				return "", ErrNotThisBox
			}
		}
	}
	switch k := KindOf(h, domainName, serverDomain, ""); k {
	case KindDash, KindDot, KindApex:
		if domainName == "" || serverDomain == "" {
			return "", ErrNotThisBox
		}
		return k, nil
	case KindSslip:
		ip := strings.ToLower(ipDash)
		if ip != "" && (h == ip+".sslip.io" || strings.HasSuffix(h, "-"+ip+".sslip.io") || strings.HasSuffix(h, "."+ip+".sslip.io")) {
			return k, nil
		}
	}
	return "", ErrNotThisBox
}

// SeenOnBox describes the certificate the box itself serves on host (dot,
// apex and sslip names: the box holds the key, so its local handshake is what
// the browser gets).
func SeenOnBox(host string, k Kind, c *x509.Certificate, probeErr error) Seen {
	s := Seen{Host: host, Kind: k, Holder: HolderBox, EndToEnd: true}
	if c == nil {
		s.Holder = HolderUnknown
		s.EndToEnd = false
		if probeErr != nil {
			s.Error = probeErr.Error()
		}
		return s
	}
	fill(&s, c)
	s.Trusted = letsEncrypt.MatchString(strings.Join(c.Issuer.Organization, " ")) || letsEncrypt.MatchString(c.Issuer.CommonName)
	return s
}

// SeenOutside describes what a visitor's handshake (ProbeOutsideLeaf) got on a
// name whose TLS ends before the box (dash names).
func SeenOutside(o Outside, leaf *x509.Certificate) Seen {
	s := Seen{Host: o.Host, Kind: o.Kind, Holder: o.Holder, Trusted: o.Verified, Error: o.Error}
	if s.Holder == "" {
		s.Holder = HolderUnknown
	}
	s.EndToEnd = s.Holder == HolderBox
	if leaf != nil {
		fill(&s, leaf)
		if o.Verified {
			s.Error = ""
		}
	}
	return s
}

func fill(s *Seen, c *x509.Certificate) {
	s.IssuedTo = c.Subject.CommonName
	if s.IssuedTo == "" && len(c.DNSNames) > 0 {
		s.IssuedTo = c.DNSNames[0]
	}
	s.DNSNames = c.DNSNames
	s.Issuer = issuerName(c)
	nb, na := c.NotBefore, c.NotAfter
	s.NotBefore, s.NotAfter = &nb, &na
	s.Fingerprint = Fingerprint(c)
}
