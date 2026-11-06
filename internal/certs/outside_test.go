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
	const (
		dash = "files-alice.nsl.sh"
		dot  = "files.alice.nsl.sh"
		apex = "alice.nsl.sh"
	)
	cases := []struct {
		name string
		c    *x509.Certificate
		k    Kind
		host string
		want Holder
	}{
		{"cloudflare issuer", mk("Cloudflare, Inc.", "Cloudflare Inc ECC CA-3", "nsl.sh", "*.nsl.sh"), KindDash, dash, HolderCloudflare},
		{"edge cert from Google Trust Services", mk("Google Trust Services", "WE1", "nsl.sh", "*.nsl.sh"), KindDash, dash, HolderCloudflare},
		{"edge cert from LE carries the apex", mk("Let's Encrypt", "E7", "nsl.sh", "*.nsl.sh"), KindDash, dash, HolderCloudflare},
		{"gateway's own *.zone (LE backup)", mk("Let's Encrypt", "E7", "*.nsl.sh"), KindDash, dash, HolderGateway},
		{"gateway's own *.zone (ZeroSSL)", mk("ZeroSSL GmbH", "ZeroSSL ECC DV SSL CA 2", "*.nsl.sh"), KindDash, dash, HolderGateway},
		{"apex terminated by the gateway", mk("ZeroSSL GmbH", "ZeroSSL ECC DV SSL CA 2", "*.nsl.sh"), KindApex, apex, HolderGateway},
		{"apex through Cloudflare (not delegated)", mk("Google Trust Services", "WE1", "nsl.sh", "*.nsl.sh"), KindApex, apex, HolderCloudflare},
		{"box wildcard on a dot name", mk("Let's Encrypt", "R11", "*.alice.nsl.sh"), KindDot, dot, HolderBox},
		{"box apex", mk("Let's Encrypt", "R11", "alice.nsl.sh"), KindApex, apex, HolderBox},
		{"gateway fallback on a dot name", mk("ZeroSSL GmbH", "ZeroSSL ECC DV SSL CA 2", "*.alice.nsl.sh"), KindDot, dot, HolderGateway},
		{"someone else's wildcard", mk("ZeroSSL GmbH", "ZeroSSL ECC DV SSL CA 2", "*.bob.nsl.sh"), KindDot, dot, HolderUnknown},
		{"zone-wide cert on a dot name is not the box's", mk("Let's Encrypt", "E7", "*.nsl.sh"), KindDot, dot, HolderUnknown},
		{"mesh CA", mk("NSL.SH", "Mesh Router CA", "*.alice.nsl.sh"), KindDot, dot, HolderUnknown},
	}
	for _, c := range cases {
		if got := HolderOf(c.c, c.k, c.host, "nsl.sh"); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
	if HolderOf(nil, KindDash, dash, "nsl.sh") != HolderUnknown {
		t.Error("nil leaf")
	}
}

func TestAgainstBox(t *testing.T) {
	cases := []struct {
		name string
		was  Holder
		box  Box
		want Holder
	}{
		{"box has no LE cert: the LE one outside is the gateway's backup", HolderBox, Box{Kind: BoxMeshCA}, HolderGateway},
		{"box unreachable locally", HolderBox, Box{Kind: BoxUnreachable}, HolderGateway},
		{"box holds LE: stays box (renewal not seen yet)", HolderBox, Box{Kind: BoxLetsEncrypt, Fingerprint: "AA"}, HolderBox},
		{"local probe not run", HolderBox, Box{}, HolderBox},
		{"gateway stays gateway", HolderGateway, Box{Kind: BoxLetsEncrypt}, HolderGateway},
	}
	for _, c := range cases {
		o := Outside{Holder: c.was}
		o.AgainstBox(c.box)
		if o.Holder != c.want {
			t.Errorf("%s: got %s, want %s", c.name, o.Holder, c.want)
		}
	}
}
