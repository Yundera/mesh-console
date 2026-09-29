package routing

import (
	"testing"

	"github.com/yundera/mesh-console/internal/backend"
)

func agentDomain(scheme string) backend.Route {
	return backend.Route{IP: "203.0.113.5", Port: 443, Priority: 1, Source: "agent", Scheme: scheme, Type: "domain", Domain: "203-0-113-5.nip.io"}
}

func tunnel(scheme string) backend.Route {
	return backend.Route{IP: "10.0.0.2", Port: 80, Priority: 2, Source: "tunnel", Scheme: scheme}
}

func TestInfer(t *testing.T) {
	cases := []struct {
		name          string
		routes        []backend.Route
		ttl           int
		mode, gw, cfw Mode
	}{
		{"nothing registered", nil, -2, ModeOffline, ModeOffline, ModeOffline},
		{"agent and tunnel", []backend.Route{tunnel("https"), tunnel("http"), agentDomain("https"), agentDomain("http")}, 500, ModeDirect, ModeDirect, ModeDirect},
		{"tunnel only", []backend.Route{tunnel("https"), tunnel("http")}, 500, ModeTunnel, ModeTunnel, ModeOffline},
		{"agent only", []backend.Route{agentDomain("https")}, 500, ModeDirect, ModeDirect, ModeDirect},
		{
			"agent ip route: nginx direct, worker falls to tunnel domain-less = offline",
			[]backend.Route{{IP: "203.0.113.5", Port: 443, Priority: 1, Source: "agent", Type: "ip"}, tunnel("https")},
			500, ModeDirect, ModeDirect, ModeOffline,
		},
		{
			"missing priority sorts last",
			[]backend.Route{{Source: "agent", Scheme: "https", Type: "domain", Domain: "x.nip.io"}, tunnel("https")},
			500, ModeTunnel, ModeTunnel, ModeDirect,
		},
		{
			"no scheme match falls back to all routes",
			[]backend.Route{tunnel("http")},
			500, ModeTunnel, ModeTunnel, ModeOffline,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := Infer(tc.routes, tc.ttl)
			if s.Mode != tc.mode || s.Gateway.Mode != tc.gw || s.Worker.Mode != tc.cfw {
				t.Fatalf("got mode=%s gateway=%s worker=%s, want %s/%s/%s", s.Mode, s.Gateway.Mode, s.Worker.Mode, tc.mode, tc.gw, tc.cfw)
			}
		})
	}
}

func TestInferFlags(t *testing.T) {
	s := Infer([]backend.Route{tunnel("https"), agentDomain("https")}, 100)
	if !s.HasAgent || !s.HasTunnel {
		t.Fatalf("flags = %+v", s)
	}
}
