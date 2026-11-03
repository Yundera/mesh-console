package certs

import (
	"strings"
	"time"
)

// MeshCert is cert0: the certificate the backend's mesh CA signs for the box
// (mesh-router-agent, data/certs/cert.pem). Since Phase 1.1 it no longer
// covers the user's domain. It proves the box's identity to the gateways
// (<label>.box.<zone>, which gateway 1.6+ requires) and serves the IP names.
type MeshCert struct {
	Subject   string    `json:"subject"`
	Issuer    string    `json:"issuer"`
	DNSNames  []string  `json:"dnsNames"`
	IPs       []string  `json:"ips"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	Renewal   Renewal   `json:"renewal"`
	// HopName is the name the gateways expect on the hop; HopOK whether cert0
	// carries it.
	HopName string `json:"hopName,omitempty"`
	HopOK   bool   `json:"hopOk"`
	// IPNames are the *.<IP_ZONE> SANs; Legacy the SANs an older backend added
	// (*.<zone>, *.nip.io) and the next renewal drops; Other the rest.
	IPNames []string `json:"ipNames"`
	Legacy  []string `json:"legacy"`
	Other   []string `json:"other"`
}

// NewMeshCert groups cert0's names by purpose. domainName / serverDomain may
// be empty (backend unreachable and no DOMAIN), then HopName stays empty.
func NewMeshCert(subject, issuer string, dnsNames, ips []string, notBefore, notAfter time.Time, domainName, serverDomain, ipZone string, now time.Time) MeshCert {
	m := MeshCert{
		Subject: subject, Issuer: issuer, DNSNames: dnsNames, IPs: ips,
		NotBefore: notBefore, NotAfter: notAfter, Renewal: RenewalOf(notBefore, notAfter, now),
		IPNames: []string{}, Legacy: []string{}, Other: []string{},
	}
	if m.IPs == nil {
		m.IPs = []string{}
	}
	if domainName != "" && serverDomain != "" {
		m.HopName = strings.ToLower(domainName + ".box." + serverDomain)
	}
	zone := strings.ToLower(serverDomain)
	for _, n := range dnsNames {
		l := strings.ToLower(n)
		switch {
		case m.HopName != "" && l == m.HopName:
			m.HopOK = true
		case ipZone != "" && (l == "*."+strings.ToLower(ipZone) || strings.HasSuffix(l, "."+strings.ToLower(ipZone))):
			m.IPNames = append(m.IPNames, n)
		case strings.HasSuffix(l, ".nip.io") || (zone != "" && l == "*."+zone):
			m.Legacy = append(m.Legacy, n)
		default:
			m.Other = append(m.Other, n)
		}
	}
	return m
}
