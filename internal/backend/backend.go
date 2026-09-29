// Package backend is a client for the mesh-router-backend GETs the console
// reads. All of them are unauthenticated (RouterAPI.ts), so no credential is
// ever sent from here.
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Route mirrors the backend's Route shape (mesh-router-backend src/services/Routes.ts).
type Route struct {
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	Priority     int    `json:"priority"`
	Source       string `json:"source"`
	Scheme       string `json:"scheme,omitempty"`
	TargetScheme string `json:"targetScheme,omitempty"`
	Type         string `json:"type,omitempty"`
	Domain       string `json:"domain,omitempty"`
}

type Version struct {
	Version          int    `json:"version"`
	MinClientVersion int    `json:"minClientVersion"`
	ServerDomain     string `json:"serverDomain"`
}

type DomainInfo struct {
	DomainName   string `json:"domainName"`
	ServerDomain string `json:"serverDomain"`
}

// Resolution is GET /resolve/v2/:domain — how the gateways see this box.
type Resolution struct {
	UserID         string  `json:"userId"`
	DomainName     string  `json:"domainName"`
	ServerDomain   string  `json:"serverDomain"`
	Routes         []Route `json:"routes"`
	RoutesTTL      int     `json:"routesTtl"`
	LastSeenOnline *string `json:"lastSeenOnline"`
}

type Client struct {
	Base string // e.g. https://api.nsl.sh/router/api
	HTTP *http.Client
}

func New(base string) *Client {
	return &Client{Base: base, HTTP: &http.Client{Timeout: 8 * time.Second}}
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	var v Version
	return v, c.get(ctx, "/version", &v)
}

func (c *Client) Domain(ctx context.Context, userID string) (DomainInfo, error) {
	var d DomainInfo
	return d, c.get(ctx, "/domain/"+url.PathEscape(userID), &d)
}

func (c *Client) Resolve(ctx context.Context, domainName string) (Resolution, error) {
	var r Resolution
	return r, c.get(ctx, "/resolve/v2/"+url.PathEscape(domainName), &r)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	// GET /domain answers "not found" with status 280 (sic), so anything but a
	// plain 200 is treated as an error rather than trusting the 2xx range.
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("backend %s: %d %s", path, resp.StatusCode, e.Error)
	}
	return json.Unmarshal(body, out)
}
