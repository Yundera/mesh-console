package probe

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Entry is where a visitor's connection lands, which is what the DNS answer for
// the name decided.
type Entry string

const (
	EntryBox        Entry = "box"        // the name answers this box's own IP
	EntryCloudflare Entry = "cloudflare" // a Cloudflare edge, then the worker
	EntryGateway    Entry = "gateway"    // the mesh gateway, answered by the nameserver
	EntryUnknown    Entry = "unknown"
)

// Lane is the whole path, as the Diagnostics route map draws it.
type Lane string

const (
	LaneBoxDirect     Lane = "box-direct"     // straight to the box
	LaneEdgeDirect    Lane = "edge-direct"    // worker → box by name (ip.<zone>, sslip.io, nip.io)
	LaneGatewayAgent  Lane = "gateway-agent"  // gateway → box's public IP (agent route)
	LaneGatewayTunnel Lane = "gateway-tunnel" // gateway → WireGuard tunnel
	LaneUnknown       Lane = "unknown"
)

// Trace is one request to a public name of this box, made the way a visitor
// makes it, with the route the gateways report for it.
type Trace struct {
	Host      string   `json:"host"`
	Resolved  []string `json:"resolved"`
	Connected string   `json:"connected,omitempty"`
	Status    int      `json:"status,omitempty"`
	// Route is X-Mesh-Route as returned (the worker and the gateway add it when
	// the request carries X-Mesh-Trace); empty when nothing on the way did.
	Route     []string  `json:"route,omitempty"`
	CFRay     bool      `json:"cfRay"`
	Entry     Entry     `json:"entry"`
	Hops      []string  `json:"hops"`
	Lane      Lane      `json:"lane"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

// Public resolvers, so the answer is the one visitors get rather than Docker's
// embedded DNS or the host's /etc/hosts.
var publicResolvers = []string{"1.1.1.1:53", "8.8.8.8:53"}

// TraceHost resolves host through public DNS, then requests https://host/ on the
// first IPv4 answer with X-Mesh-Trace set. The certificate is verified: an
// untrusted one on a public name is a finding, not noise. A redirect (the SSO
// login) is a routed answer and is not followed.
func TraceHost(ctx context.Context, host string, boxIPs []string) Trace {
	t := Trace{Host: host, CheckedAt: time.Now().UTC(), Hops: []string{}}
	ips, err := lookupPublic(ctx, host)
	if err != nil {
		t.Error = "DNS: " + err.Error()
		t.Entry, t.Lane = EntryUnknown, LaneUnknown
		return t
	}
	t.Resolved = ips
	t.Connected = ips[0]

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(t.Connected, "443"))
		},
	}
	defer tr.CloseIdleConnections()
	cl := &http.Client{
		Transport:     tr,
		Timeout:       8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		t.Error = err.Error()
		t.Entry, t.Lane = EntryUnknown, LaneUnknown
		return t
	}
	req.Header.Set("X-Mesh-Trace", "1")
	resp, err := cl.Do(req)
	if err != nil {
		t.Error = err.Error()
	} else {
		resp.Body.Close()
		t.Status = resp.StatusCode
		t.CFRay = resp.Header.Get("Cf-Ray") != ""
		t.Route = splitRoute(resp.Header.Get("X-Mesh-Route"))
	}
	t.Entry, t.Hops, t.Lane = Classify(t.Connected, boxIPs, t.CFRay, t.Route)
	return t
}

func lookupPublic(ctx context.Context, host string) ([]string, error) {
	var last error
	for _, srv := range publicResolvers {
		r := &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, srv)
		}}
		// Trailing dot: a fully qualified name, no search domains.
		ips, err := r.LookupIP(ctx, "ip4", host+".")
		if err == nil && len(ips) > 0 {
			out := make([]string, len(ips))
			for i, ip := range ips {
				out[i] = ip.String()
			}
			return out, nil
		}
		if err == nil {
			err = errors.New("no IPv4 address")
		}
		last = err
	}
	return nil, last
}

func splitRoute(h string) []string {
	var out []string
	for _, p := range strings.Split(h, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Classify reads a trace the way the route map draws it. The box's Caddy adds no
// X-Mesh-Route, so a direct connection is recognised by the IP it reached; the
// worker reports its whole path (cf-worker,…,pcs) and the gateway alone reports
// the route it took (agent,pcs / tunnel,pcs).
func Classify(connected string, boxIPs []string, cfRay bool, route []string) (Entry, []string, Lane) {
	hops := []string{}
	for _, h := range route {
		if h != "cf-worker" && h != "pcs" {
			hops = append(hops, h)
		}
	}
	switch {
	case connected != "" && slices.Contains(boxIPs, connected):
		return EntryBox, []string{}, LaneBoxDirect
	case cfRay || (len(route) > 0 && route[0] == "cf-worker"):
		return EntryCloudflare, hops, laneOf(hops, true)
	case len(hops) > 0:
		return EntryGateway, hops, laneOf(hops, false)
	default:
		return EntryUnknown, hops, LaneUnknown
	}
}

// laneOf picks the lane from the hops. Through the worker, anything that is not
// the gateway is the worker reaching the box by name.
func laneOf(hops []string, viaWorker bool) Lane {
	gateway := !viaWorker || slices.Contains(hops, "gateway") || slices.Contains(hops, "gateway-fallback")
	switch {
	case gateway && slices.Contains(hops, "tunnel"):
		return LaneGatewayTunnel
	case gateway && (slices.Contains(hops, "agent") || slices.Contains(hops, "direct")):
		return LaneGatewayAgent
	case gateway:
		return LaneUnknown
	case len(hops) > 0:
		return LaneEdgeDirect
	default:
		return LaneUnknown
	}
}
