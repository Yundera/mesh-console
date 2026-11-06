package certs

import (
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"strings"
	"testing"
)

func TestAddressKind(t *testing.T) {
	cases := []struct {
		host string
		want Kind // "" = refused
	}{
		{"mesh-console-alice.nsl.sh", KindDash},
		{"Mesh-Console-Alice.NSL.sh.", KindDash},
		{"mesh-console.alice.nsl.sh", KindDot},
		{"alice.nsl.sh", KindApex},
		{"mesh-console-1-2-3-4.sslip.io", KindSslip},
		{"1-2-3-4.sslip.io", KindSslip},
		{"mesh-console-9-9-9-9.sslip.io", ""}, // another box's IP
		{"bob.nsl.sh", ""},
		{"mesh-console-bob.nsl.sh", ""},
		{"example.com", ""},
		{"169.254.169.254", ""},
		{"alice.nsl.sh:8080", ""},
		{"a..alice.nsl.sh", ""},
		{"x_y.alice.nsl.sh", KindDot},
		{"", ""},
	}
	for _, tc := range cases {
		got, err := AddressKind(tc.host, "alice", "nsl.sh", "1-2-3-4")
		if tc.want == "" {
			if err == nil {
				t.Errorf("%q: accepted as %s", tc.host, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q, %v; want %s", tc.host, got, err, tc.want)
		}
	}
	if _, err := AddressKind("mesh-console-alice.nsl.sh", "", "", "1-2-3-4"); err == nil {
		t.Error("dash name accepted without a known domain")
	}
}

func TestFingerprint(t *testing.T) {
	c := &x509.Certificate{Raw: []byte("certificate")}
	fp := Fingerprint(c)
	sum := sha256.Sum256(c.Raw)
	if len(fp) != len(sum)*3-1 || strings.ToUpper(fp) != fp || fp[2] != ':' {
		t.Fatalf("bad format %q", fp)
	}
	if strings.ReplaceAll(fp, ":", "") != strings.ToUpper(hexOf(sum[:])) {
		t.Fatalf("wrong digest %q", fp)
	}
	if Fingerprint(nil) != "" {
		t.Fatal("nil cert has a fingerprint")
	}
}

func hexOf(b []byte) string {
	const d = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, v := range b {
		out = append(out, d[v>>4], d[v&0xf])
	}
	return string(out)
}

func TestSeen(t *testing.T) {
	le := &x509.Certificate{
		Raw:      []byte("le"),
		Issuer:   pkix.Name{Organization: []string{"Let's Encrypt"}, CommonName: "E6"},
		DNSNames: []string{"*.alice.nsl.sh", "alice.nsl.sh"},
	}
	s := SeenOnBox("mesh-console.alice.nsl.sh", KindDot, le, nil)
	if !s.EndToEnd || !s.Trusted || s.IssuedTo != "*.alice.nsl.sh" || s.Fingerprint == "" {
		t.Fatalf("box LE: %+v", s)
	}
	local := &x509.Certificate{Raw: []byte("x"), Issuer: pkix.Name{CommonName: "Caddy Local Authority"}}
	if s := SeenOnBox("a.alice.nsl.sh", KindDot, local, nil); s.Trusted || !s.EndToEnd {
		t.Fatalf("box fallback: %+v", s)
	}
	if s := SeenOnBox("a.alice.nsl.sh", KindDot, nil, nil); s.EndToEnd || s.Holder != HolderUnknown {
		t.Fatalf("no cert: %+v", s)
	}
	gw := &x509.Certificate{Raw: []byte("gw"), Subject: pkix.Name{CommonName: "*.nsl.sh"}, Issuer: le.Issuer, DNSNames: []string{"*.nsl.sh"}}
	o := Outside{Host: "mesh-console-alice.nsl.sh", Kind: KindDash, Holder: HolderGateway, Verified: true}
	if s := SeenOutside(o, gw); s.EndToEnd || !s.Trusted || s.IssuedTo != "*.nsl.sh" || s.Holder != HolderGateway {
		t.Fatalf("gateway: %+v", s)
	}
}
