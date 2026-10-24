package certs

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"testing"
	"time"
)

func TestClassifyBox(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	mk := func(org, cn string, names ...string) *x509.Certificate {
		return &x509.Certificate{
			Issuer:    pkix.Name{Organization: []string{org}, CommonName: cn},
			DNSNames:  names,
			NotBefore: now.Add(-24 * time.Hour),
			NotAfter:  now.Add(60 * 24 * time.Hour),
		}
	}
	cases := []struct {
		name     string
		c        *x509.Certificate
		kind     BoxKind
		wildcard bool
		method   string
	}{
		{"LE wildcard is dns-01", mk("Let's Encrypt", "R11", "alice.nsl.sh", "*.alice.nsl.sh"), BoxLetsEncrypt, true, "dns-01"},
		{"LE single name says nothing of the method", mk("Let's Encrypt", "R11", "alice.nsl.sh"), BoxLetsEncrypt, false, ""},
		{"mesh CA", mk("", "Mesh Router CA", "alice.nsl.sh", "*.alice.nsl.sh"), BoxMeshCA, true, ""},
		{"caddy internal", mk("", "Caddy Local Authority - ECC Intermediate", "alice.nsl.sh"), BoxOther, false, ""},
		{"wildcard of another domain", mk("Let's Encrypt", "R11", "*.nsl.sh"), BoxLetsEncrypt, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := ClassifyBox("alice.nsl.sh", tc.c, nil, "Mesh Router CA", now)
			if b.Kind != tc.kind || b.Wildcard != tc.wildcard || b.Method != tc.method || b.Renewal != RenewalOK {
				t.Fatalf("got %+v", b)
			}
		})
	}
	b := ClassifyBox("alice.nsl.sh", nil, errors.New("connection refused"), "", now)
	if b.Kind != BoxUnreachable || b.Error != "connection refused" {
		t.Fatalf("unreachable: %+v", b)
	}
}
