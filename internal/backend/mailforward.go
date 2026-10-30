package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// MailForward is the backend's view of the box's mail forwarding
// (mesh-router-backend UserMail.ts, GET|PUT|DELETE /email/forward/:userid/:sig):
// mail to <user>@<zone> goes to Target once its owner confirmed the link sent there.
type MailForward struct {
	// Enabled is false when the server receives no mail at all.
	Enabled bool   `json:"enabled"`
	Address string `json:"address"`
	State   string `json:"state"` // off | pending | active | paused
	Target  string `json:"target,omitempty"`
	Reason  string `json:"reason,omitempty"` // why paused
}

// ErrMailForwardUnsupported means the backend predates mail forwarding.
var ErrMailForwardUnsupported = errors.New("this mesh server does not offer mail forwarding yet")

// APIError is the backend's own refusal (a bad address, the daily limit, …):
// its message is meant for the owner.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }

func (c *Client) MailForward(ctx context.Context, userID, sig string) (MailForward, error) {
	return c.mailForward(ctx, http.MethodGet, userID, sig, nil)
}

// SetMailForward sets the forwarding address; the backend mails it a confirmation link.
func (c *Client) SetMailForward(ctx context.Context, userID, sig, target string) (MailForward, error) {
	return c.mailForward(ctx, http.MethodPut, userID, sig, map[string]string{"target": target})
}

func (c *Client) ClearMailForward(ctx context.Context, userID, sig string) (MailForward, error) {
	return c.mailForward(ctx, http.MethodDelete, userID, sig, nil)
}

// mailForward never puts the request URL in an error: it carries the signature.
func (c *Client) mailForward(ctx context.Context, method, userID, sig string, body any) (MailForward, error) {
	var f MailForward
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return f, err
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/email/forward/"+url.PathEscape(userID)+"/"+url.PathEscape(sig), payload)
	if err != nil {
		return f, errors.New("invalid mail forwarding request")
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return f, fmt.Errorf("mesh server unreachable: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return f, err
	}
	if resp.StatusCode == http.StatusNotFound {
		return f, ErrMailForwardUnsupported
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return f, &APIError{Status: resp.StatusCode, Message: e.Error}
	}
	return f, json.Unmarshal(raw, &f)
}
