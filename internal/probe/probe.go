// Package probe gathers local evidence the backend cannot see.
package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type RootDomain struct {
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
	// Catchall is true when Caddy answered from the injected catch-all route
	// rather than the Caddyfile's root-domain block (X-Mesh-Catchall header).
	Catchall bool `json:"catchall"`
}

// Root requests https://<caddyHost>/ with Host/SNI set to domain — the same
// check ensure-root-domain.sh makes against 127.0.0.1 on the host. It goes to
// Caddy over the pcs network, so it tests the local half only (Caddy → default
// app), not the gateway. Certificate verification is off: the root domain is
// served with the mesh CA's cert, which this container does not trust, and the
// question is "does it route", not "is the cert valid".
func Root(ctx context.Context, caddyHost, domain string) RootDomain {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: domain, InsecureSkipVerify: true}, //nolint:gosec // see above
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, net.JoinHostPort(caddyHost, "443"))
		},
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
		// A redirect (e.g. to the SSO login) is a routed answer — report it as is.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/", nil)
	if err != nil {
		return RootDomain{Error: err.Error()}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return RootDomain{Error: err.Error()}
	}
	resp.Body.Close()
	return RootDomain{Status: resp.StatusCode, Catchall: resp.Header.Get("X-Mesh-Catchall") != ""}
}

type Cert struct {
	Subject  string    `json:"subject"`
	DNSNames []string  `json:"dnsNames"`
	NotAfter time.Time `json:"notAfter"`
	Issuer   string    `json:"issuer"`
}

// AgentCert reads the certificate mesh-router-agent obtained from the backend
// (data/certs/cert.pem, shared with Caddy as the gateway_tls cert). It is what
// the gateway and CF worker verify when they connect to this box directly.
func AgentCert(meshDir string) (*Cert, error) {
	b, err := os.ReadFile(filepath.Join(meshDir, "data", "certs", "cert.pem"))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("cert.pem: no PEM block")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return &Cert{Subject: c.Subject.CommonName, DNSNames: c.DNSNames, NotAfter: c.NotAfter, Issuer: c.Issuer.CommonName}, nil
}
