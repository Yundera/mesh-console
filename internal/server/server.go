// Package server wires the HTTP API and the embedded single-page app.
package server

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/yundera/mesh-console/internal/auth"
	"github.com/yundera/mesh-console/internal/backend"
	"github.com/yundera/mesh-console/internal/config"
	"github.com/yundera/mesh-console/internal/dockerx"
	"github.com/yundera/mesh-console/internal/mail"
	"github.com/yundera/mesh-console/internal/meshenv"
	"github.com/yundera/mesh-console/internal/update"
)

// Version is set at build time (-ldflags "-X .../server.Version=...").
var Version = "dev"

type Server struct {
	cfg    config.Config
	authn  auth.Authenticator
	docker *dockerx.Client // nil when the socket is unavailable
	github *update.GitHub

	// idCache holds the backend's answer for who this box is. It changes only
	// when the user renames their domain, so a minute is plenty.
	idMu    sync.Mutex
	idCache *identityCache

	// mailer sends the Email page's test message; lastTestMail rate-limits it.
	mailer       mail.Sender
	mailMu       sync.Mutex
	lastTestMail time.Time
}

type identityCache struct {
	at           time.Time
	backendBase  string
	domainName   string
	serverDomain string
	err          error
}

func New(cfg config.Config, uiFS fs.FS) http.Handler {
	return newServer(cfg).routes(uiFS)
}

func newServer(cfg config.Config) *Server {
	s := &Server{cfg: cfg, github: update.NewGitHub(), mailer: mail.SMTPSender{Addr: cfg.SMTPAddr}}
	s.authn = auth.Authenticator{Secret: []byte(cfg.AssertionSecret), Audience: cfg.AssertionAudience}
	if cfg.AssertionSecret == "" && cfg.DevIdentity != "" && cfg.Env == "development" {
		log.Printf("WARNING: DEV_IDENTITY=%q — every request is treated as that admin", cfg.DevIdentity)
		s.authn.Dev = &auth.Identity{User: cfg.DevIdentity, Groups: []string{"admins"}, Method: "dev"}
	} else if cfg.AssertionSecret == "" {
		log.Print("WARNING: IDENTITY_ASSERTION_SECRET is not set — every API request will be refused")
	}
	if d, err := dockerx.New(); err != nil {
		log.Printf("docker unavailable: %v", err)
	} else {
		s.docker = d
	}
	return s
}

func (s *Server) routes(uiFS fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP, middleware.Recoverer, securityHeaders)

	// The only unauthenticated route, matching the gate's ALLOWED_PATHS.
	r.Get("/api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version})
	})

	r.Route("/api", func(api chi.Router) {
		api.Use(s.authn.RequireAdmin)
		api.Use(csrfGuard)
		api.Get("/me", s.handleMe)
		api.Get("/status", s.handleStatus)
		api.Get("/overview", s.handleOverview)
		api.Get("/routing", s.handleRouting)
		api.Get("/stack", s.handleStack)
		api.Get("/update", s.handleUpdate)
		api.Post("/update/run", s.handleUpdateRun)
		api.Post("/update/channel", s.handleSetUpdateChannel)
		api.Get("/selfcheck", s.handleSelfCheck)
		api.Get("/domain", s.handleDomain)
		api.Post("/domain/default-app", s.handleSetDefaultApp)
		api.Get("/certificates", s.handleCertificates)
		api.Get("/mail", s.handleMail)
		api.Post("/mail/test", s.handleMailTest)
		api.Get("/migration", s.handleMigration)
		api.Post("/migration/key", s.handleMigrationKey)
		api.Post("/migration/preflight", s.handleMigrationPreflight)
		api.Post("/migration/start", s.handleMigrationStart)
		api.Post("/migration/cancel", s.handleMigrationCancel)
		api.NotFound(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusNotFound, "not found")
		})
	})

	r.Handle("/*", spaHandler(uiFS))
	return r
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"identity": id, "version": Version})
}

// env reads the mesh .env fresh.
func (s *Server) env() (meshenv.Env, error) {
	return meshenv.Load(s.cfg.MeshDir)
}

// identity resolves domainName/serverDomain from the backend, falling back to
// splitting DOMAIN when the backend is unreachable (DOMAIN is always
// <domainName>.<serverDomain>).
func (s *Server) identity(ctx context.Context, env meshenv.Env) (base, domainName, serverDomain string, err error) {
	p, perr := env.Provider()
	fallbackName, fallbackServer, _ := strings.Cut(env.Get("DOMAIN"), ".")
	if perr != nil {
		return "", fallbackName, fallbackServer, perr
	}
	s.idMu.Lock()
	c := s.idCache
	s.idMu.Unlock()
	if c != nil && c.backendBase == p.APIBase() && time.Since(c.at) < time.Minute {
		if c.err != nil {
			return c.backendBase, fallbackName, fallbackServer, c.err
		}
		return c.backendBase, c.domainName, c.serverDomain, nil
	}
	cl := backend.New(p.APIBase())
	d, derr := cl.Domain(ctx, p.UserID)
	nc := &identityCache{at: time.Now(), backendBase: p.APIBase(), domainName: d.DomainName, serverDomain: d.ServerDomain, err: derr}
	s.idMu.Lock()
	s.idCache = nc
	s.idMu.Unlock()
	if derr != nil {
		return nc.backendBase, fallbackName, fallbackServer, derr
	}
	return nc.backendBase, d.DomainName, d.ServerDomain, nil
}

// ---- plumbing ------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// csrfGuard requires a custom header on state-changing requests. A cross-site
// page cannot set it without a CORS preflight, and this server answers none,
// so the gate's session cookie alone can never trigger a host action.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Mesh-Console") != "1" {
			writeError(w, http.StatusForbidden, "missing X-Mesh-Console header")
			return
		}
		next.ServeHTTP(w, r)
	})
}
