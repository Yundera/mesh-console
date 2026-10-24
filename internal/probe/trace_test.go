package probe

import (
	"slices"
	"testing"
)

func TestClassify(t *testing.T) {
	box := []string{"80.241.218.30"}
	cases := []struct {
		name      string
		connected string
		cfRay     bool
		route     []string
		entry     Entry
		hops      []string
		lane      Lane
	}{
		// wisera, 2026-10-24: the nameserver answers the box, Caddy adds no header
		{"nameserver answers the box", "80.241.218.30", false, nil, EntryBox, []string{}, LaneBoxDirect},
		{"worker ip.<zone>", "188.114.96.2", true, []string{"cf-worker", "ip-dns", "pcs"}, EntryCloudflare, []string{"ip-dns"}, LaneEdgeDirect},
		{"worker sslip fallback", "188.114.96.2", true, []string{"cf-worker", "ip-dns-fail", "sslip.io", "pcs"}, EntryCloudflare, []string{"ip-dns-fail", "sslip.io"}, LaneEdgeDirect},
		{"worker forced tunnel", "188.114.96.2", true, []string{"cf-worker", "gateway", "tunnel", "pcs"}, EntryCloudflare, []string{"gateway", "tunnel"}, LaneGatewayTunnel},
		{"worker owner force, gateway agent", "188.114.96.2", true, []string{"cf-worker", "owner-force", "gateway", "agent", "pcs"}, EntryCloudflare, []string{"owner-force", "gateway", "agent"}, LaneGatewayAgent},
		{"cf-ray stripped, header still says worker", "188.114.96.2", false, []string{"cf-worker", "ip-dns", "pcs"}, EntryCloudflare, []string{"ip-dns"}, LaneEdgeDirect},
		{"nameserver answers the gateway, tunnel", "151.115.96.14", false, []string{"tunnel", "pcs"}, EntryGateway, []string{"tunnel"}, LaneGatewayTunnel},
		{"nameserver answers the gateway, agent", "151.115.96.14", false, []string{"agent", "pcs"}, EntryGateway, []string{"agent"}, LaneGatewayAgent},
		{"no header, not the box", "203.0.113.9", false, nil, EntryUnknown, []string{}, LaneUnknown},
		{"worker without a downstream hop", "188.114.96.2", true, []string{"cf-worker", "pcs"}, EntryCloudflare, []string{}, LaneUnknown},
		{"box IP wins over a stray header", "80.241.218.30", false, []string{"agent", "pcs"}, EntryBox, []string{}, LaneBoxDirect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, hops, lane := Classify(tc.connected, box, tc.cfRay, tc.route)
			if e != tc.entry || !slices.Equal(hops, tc.hops) || lane != tc.lane {
				t.Fatalf("got %s %v %s, want %s %v %s", e, hops, lane, tc.entry, tc.hops, tc.lane)
			}
		})
	}
}

func TestSplitRoute(t *testing.T) {
	if got := splitRoute(" cf-worker, gateway ,tunnel,pcs,"); !slices.Equal(got, []string{"cf-worker", "gateway", "tunnel", "pcs"}) {
		t.Fatalf("got %v", got)
	}
	if got := splitRoute(""); got != nil {
		t.Fatalf("empty: %v", got)
	}
}
