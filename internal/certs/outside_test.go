package certs

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
)

func TestHolderOf(t *testing.T) {
	mk := func(org, cn string, names ...string) *x509.Certificate {
		return &x509.Certificate{Issuer: pkix.Name{Organization: []string{org}, CommonName: cn}, DNSNames: names}
	}
	cases := []struct {
		name string
		c    *x509.Certificate
		k    Kind
		want Holder
	}{
		{"cloudflare issuer", mk("Cloudflare, Inc.", "Cloudflare Inc ECC CA-3", "nsl.sh", "*.nsl.sh"), KindDash, HolderCloudflare},
		{"edge cert from Google Trust Services", mk("Google Trust Services", "WE1", "nsl.sh", "*.nsl.sh"), KindDash, HolderCloudflare},
		{"edge cert from LE carries the apex", mk("Let's Encrypt", "E7", "nsl.sh", "*.nsl.sh"), KindDash, HolderCloudflare},
		{"gateway's own *.zone", mk("Let's Encrypt", "E7", "*.nsl.sh"), KindDash, HolderGateway},
		{"box wildcard on a dot name", mk("Let's Encrypt", "R11", "*.alice.nsl.sh"), KindDot, HolderBox},
		{"box apex", mk("Let's Encrypt", "R11", "alice.nsl.sh"), KindApex, HolderBox},
		{"zone-wide cert on a dot name is not the box's", mk("Let's Encrypt", "E7", "*.nsl.sh"), KindDot, HolderUnknown},
		{"mesh CA", mk("NSL.SH", "Mesh Router CA", "*.ip.nsl.sh"), KindDot, HolderUnknown},
	}
	for _, c := range cases {
		if got := HolderOf(c.c, c.k, "nsl.sh"); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
	if HolderOf(nil, KindDash, "nsl.sh") != HolderUnknown {
		t.Error("nil leaf")
	}
}
