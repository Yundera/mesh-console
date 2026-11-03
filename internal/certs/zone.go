package certs

import (
	"os"
	"path/filepath"
	"strings"
)

// ZoneState is where the box's own domain stands, from the browser's side.
type ZoneState string

const (
	// The zone is not delegated: the apex is served with cert0 (only the
	// gateway and the Worker trust it) and the dot names with Caddy's internal
	// wildcard. Dot names cannot be used in a browser.
	ZoneNotDelegated ZoneState = "not-delegated"
	// Delegated per the backend, but Caddy has not switched to the DNS-01
	// issuer yet (it does at its next config generation).
	ZonePending ZoneState = "pending"
	// Caddy is on the DNS-01 issuer but does not serve a Let's Encrypt
	// certificate for the apex or the wildcard yet.
	ZoneIssuing ZoneState = "issuing"
	ZoneOK      ZoneState = "ok"
	ZoneOverdue ZoneState = "overdue"
	ZoneExpired ZoneState = "expired"
)

// Zone is the "Your domain" card: the apex and the dot-name wildcard.
type Zone struct {
	Domain string `json:"domain"`
	// Live is the backend's dns.live (the parent zone really delegates to the
	// mesh nameservers); nil when the backend did not answer.
	Live *bool `json:"live"`
	// Applied is what mesh-router-caddy last applied (data/mesh-zone.state:
	// 1 = DNS-01 issuer); nil when the file is absent (older Caddy image).
	Applied *bool  `json:"applied"`
	Answer  string `json:"answer,omitempty"` // direct | gateway (the nameservers' answer)
	// Apex is the certificate served on <domain>; Wildcard the one served on
	// WildcardHost, a dot name (covered by the *.<domain> site).
	Apex         Box       `json:"apex"`
	WildcardHost string    `json:"wildcardHost"`
	Wildcard     Box       `json:"wildcard"`
	State        ZoneState `json:"state"`
	Reason       string    `json:"reason,omitempty"`
	ReasonDetail string    `json:"reasonDetail,omitempty"`
}

// ReadApplied reads mesh-router-caddy's last applied zone state.
func ReadApplied(caddyDataDir string) *bool {
	b, err := os.ReadFile(filepath.Join(caddyDataDir, "mesh-zone.state"))
	if err != nil {
		return nil
	}
	v := strings.TrimSpace(string(b)) == "1"
	return &v
}

// Delegated is the box's best view of delegation: the backend when it
// answered, else what Caddy last applied.
func (z *Zone) Delegated() bool {
	switch {
	case z.Live != nil:
		return *z.Live
	case z.Applied != nil:
		return *z.Applied
	}
	return false
}

// Evaluate sets State and, for a zone that should be on Let's Encrypt but is
// not, the reason read from Caddy's log (apexLine / wildcardLine: the latest
// certificate error naming <domain> / *.<domain>).
func (z *Zone) Evaluate(apexLine, wildcardLine string) {
	z.Reason, z.ReasonDetail = "", ""
	if !z.Delegated() {
		z.State = ZoneNotDelegated
		return
	}
	if z.Applied != nil && !*z.Applied {
		z.State = ZonePending
		return
	}
	apexLE, wildLE := z.Apex.Kind == BoxLetsEncrypt, z.Wildcard.Kind == BoxLetsEncrypt
	if !apexLE || !wildLE {
		z.State = ZoneIssuing
		// The wildcard carries the dot names, so its reason comes first.
		if !wildLE {
			z.Reason, z.ReasonDetail = Reason(KindDot, z.Wildcard.Kind == BoxUnreachable, wildcardLine)
		} else {
			z.Reason, z.ReasonDetail = Reason(KindApex, z.Apex.Kind == BoxUnreachable, apexLine)
		}
		return
	}
	z.State = ZoneOK
	for _, r := range []Renewal{z.Apex.Renewal, z.Wildcard.Renewal} {
		switch {
		case r == RenewalExpired:
			z.State = ZoneExpired
		case r == RenewalOverdue && z.State == ZoneOK:
			z.State = ZoneOverdue
		}
	}
}
