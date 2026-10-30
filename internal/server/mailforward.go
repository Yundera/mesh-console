package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/yundera/mesh-console/internal/backend"
)

// ---- mail forwarding -------------------------------------------------------
//
// Mail to <user>@<zone> (and <user>+tag@, <app>.<user>@) is received by the mesh
// server and forwarded to one address the owner sets here and confirms from a link
// mailed to it (mesh-router-backend UserMail.ts). The setting lives on the server;
// the console only relays it, signed with the box's PROVIDER_STR signature.

// mailForwardView is what the Email page gets: the backend's view, or why there is none.
type mailForwardView struct {
	// Supported is false when the mesh server has no mail forwarding (older backend).
	Supported bool   `json:"supported"`
	Message   string `json:"message,omitempty"`
	*backend.MailForward
}

func (s *Server) handleMailForward(w http.ResponseWriter, r *http.Request) {
	s.mailForwardCall(w, r, func(ctx context.Context, c *backend.Client, userID, sig string) (backend.MailForward, error) {
		return c.MailForward(ctx, userID, sig)
	})
}

func (s *Server) handleSetMailForward(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	s.mailForwardCall(w, r, func(ctx context.Context, c *backend.Client, userID, sig string) (backend.MailForward, error) {
		return c.SetMailForward(ctx, userID, sig, body.Target)
	})
}

func (s *Server) handleClearMailForward(w http.ResponseWriter, r *http.Request) {
	s.mailForwardCall(w, r, func(ctx context.Context, c *backend.Client, userID, sig string) (backend.MailForward, error) {
		return c.ClearMailForward(ctx, userID, sig)
	})
}

type mailForwardFn func(ctx context.Context, c *backend.Client, userID, sig string) (backend.MailForward, error)

func (s *Server) mailForwardCall(w http.ResponseWriter, r *http.Request, call mailForwardFn) {
	env, err := s.env()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read mesh .env: "+err.Error())
		return
	}
	p, err := env.Provider()
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	sig, err := env.Signature()
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	// Setting an address sends the confirmation mail before the backend answers.
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	c := backend.New(p.APIBase())
	c.HTTP.Timeout = 20 * time.Second
	f, err := call(ctx, c, p.UserID, sig)
	var apiErr *backend.APIError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, mailForwardView{Supported: true, MailForward: &f})
	case errors.Is(err, backend.ErrMailForwardUnsupported):
		writeJSON(w, http.StatusOK, mailForwardView{Supported: false, Message: err.Error()})
	case errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 && apiErr.Status != http.StatusUnauthorized:
		writeError(w, apiErr.Status, apiErr.Message)
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized:
		writeError(w, http.StatusBadGateway, "the mesh server did not accept this box's credential")
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}
