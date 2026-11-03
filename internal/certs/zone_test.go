package certs

import (
	"strings"
	"testing"
)

func TestZoneEvaluate(t *testing.T) {
	yes, no := true, false
	le := Box{Kind: BoxLetsEncrypt, Renewal: RenewalOK}
	cases := []struct {
		name          string
		live, applied *bool
		apex, wild    Box
		want          ZoneState
	}{
		{"backend says not live", &no, &no, Box{Kind: BoxMeshCA}, Box{Kind: BoxOther}, ZoneNotDelegated},
		{"backend down, caddy applied", nil, &yes, le, le, ZoneOK},
		{"backend down, nothing known", nil, nil, le, le, ZoneNotDelegated},
		{"live, caddy not switched yet", &yes, &no, Box{Kind: BoxMeshCA}, Box{Kind: BoxOther}, ZonePending},
		{"live, older caddy without state file", &yes, nil, le, le, ZoneOK},
		{"live, wildcard still internal", &yes, &yes, le, Box{Kind: BoxOther}, ZoneIssuing},
		{"live, apex unreachable", &yes, &yes, Box{Kind: BoxUnreachable}, le, ZoneIssuing},
		{"overdue", &yes, &yes, Box{Kind: BoxLetsEncrypt, Renewal: RenewalOverdue}, le, ZoneOverdue},
		{"expired beats overdue", &yes, &yes, Box{Kind: BoxLetsEncrypt, Renewal: RenewalOverdue}, Box{Kind: BoxLetsEncrypt, Renewal: RenewalExpired}, ZoneExpired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			z := Zone{Domain: "alice.nsl.sh", Live: c.live, Applied: c.applied, Apex: c.apex, Wildcard: c.wild}
			z.Evaluate("", "")
			if z.State != c.want {
				t.Fatalf("got %s, want %s", z.State, c.want)
			}
		})
	}

	z := Zone{Live: &yes, Applied: &yes, Apex: le, Wildcard: Box{Kind: BoxOther}}
	z.Evaluate("", `ERROR {"identifier": "*.alice.nsl.sh", "error": "urn:ietf:params:acme:error:caa"}`)
	if !strings.Contains(z.Reason, "CAA") || z.ReasonDetail == "" {
		t.Fatalf("wildcard reason: %+v", z)
	}
}
