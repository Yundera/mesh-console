// Package config reads mesh-console's settings from the environment.
package config

import (
	"os"
	"path"
	"strings"
)

type Config struct {
	// Addr is the listen address. The AppShield gate is the only thing that
	// talks to it, over the pcs network.
	Addr string

	// MeshDir is the mesh stack's root (${DATA_ROOT}/AppData/mesh) as mounted
	// read-only inside this container: .env, log/mesh.log, data/certs, template/.
	MeshDir string

	// MeshHostRoot is the same directory as the HOST sees it. Host verbs run in
	// the host's mount namespace, so they need the host path, not MeshDir.
	MeshHostRoot string

	// AssertionSecret verifies the gate's X-AppShield-Assertion. Empty means
	// every API request is refused — there is no unauthenticated mode.
	AssertionSecret string
	// AssertionAudience must match the gate's APP_NAME, which AppShield derives
	// from the gate container's hostname (`mesh-console`).
	AssertionAudience string

	// DevIdentity lets a developer run the UI without a gate. Honoured only when
	// AssertionSecret is empty AND Env is "development", so a production
	// deployment that forgets the secret fails closed rather than open.
	DevIdentity string
	Env         string

	// SelfContainer names this container, so the runner can be created from the
	// exact image this process runs from. RunnerImage overrides the lookup.
	SelfContainer string
	RunnerImage   string

	// TunnelContainer / CaddyContainer are the mesh stack's containers probed for
	// local routing evidence.
	TunnelContainer string
	CaddyHost       string

	// LogFile is the self-check log as mounted here. Empty means
	// ${MeshDir}/log/mesh.log (FOSS mesh template); a Yundera PCS writes
	// log/yundera.log instead.
	LogFile string
	// TemplateScripts is the HOST path of the template's scripts directory —
	// where self-check.sh and the tools/ the console calls live. Empty means
	// ${MeshHostRoot}/scripts (the mesh template); a Yundera PCS keeps them in
	// template/scripts. See Scripts().
	TemplateScripts string
	// SelfCheckScript overrides ${TemplateScripts}/self-check.sh. Deprecated:
	// kept so a deployment that set it before TEMPLATE_SCRIPTS keeps working.
	SelfCheckScript string
	// DefaultAppEdit is an opt-out for the Domain page's default-app editor.
	// The editor also needs the template to ship tools/set-default-app.sh — the
	// template's own action, which knows where the setting is stored and what to
	// recreate — and turns itself off (read-only, with the reason) when it does
	// not.
	DefaultAppEdit bool
	// PlatformProjects are the compose projects listed as platform containers.
	PlatformProjects []string

	// MailContainer is the mail relay (mail-gateway) the Email page reads its
	// activity from; SMTPAddr is where the test email is sent.
	MailContainer string
	SMTPAddr      string
}

func FromEnv() Config {
	return Config{
		Addr:              def(os.Getenv("LISTEN_ADDR"), ":8080"),
		MeshDir:           def(os.Getenv("MESH_DIR"), "/mesh"),
		MeshHostRoot:      def(os.Getenv("MESH_HOST_ROOT"), "/DATA/AppData/mesh"),
		AssertionSecret:   strings.TrimSpace(os.Getenv("IDENTITY_ASSERTION_SECRET")),
		AssertionAudience: def(os.Getenv("IDENTITY_ASSERTION_AUDIENCE"), "mesh-console"),
		DevIdentity:       strings.TrimSpace(os.Getenv("DEV_IDENTITY")),
		Env:               def(os.Getenv("MESH_CONSOLE_ENV"), "production"),
		SelfContainer:     def(os.Getenv("SELF_CONTAINER"), "mesh-console-app"),
		RunnerImage:       strings.TrimSpace(os.Getenv("RUNNER_IMAGE")),
		TunnelContainer:   def(os.Getenv("TUNNEL_CONTAINER"), "mesh-router-tunnel"),
		CaddyHost:         def(os.Getenv("CADDY_HOST"), "mesh-router-caddy"),
		LogFile:           strings.TrimSpace(os.Getenv("LOG_FILE")),
		TemplateScripts:   strings.TrimSpace(os.Getenv("TEMPLATE_SCRIPTS")),
		SelfCheckScript:   strings.TrimSpace(os.Getenv("SELF_CHECK_SCRIPT")),
		DefaultAppEdit:    !isFalse(os.Getenv("DEFAULT_APP_EDIT")),
		PlatformProjects:  csv(def(os.Getenv("PLATFORM_PROJECTS"), "mesh,maison,mesh-console")),
		MailContainer:     def(os.Getenv("MAIL_CONTAINER"), "smtp"),
		SMTPAddr:          def(os.Getenv("SMTP_ADDR"), "smtp:587"),
	}
}

func isFalse(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "false", "0", "off", "no", "disabled":
		return true
	}
	return false
}

func csv(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func def(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}

// Scripts is the template's scripts directory as the HOST sees it.
func (c Config) Scripts() string {
	if c.TemplateScripts != "" {
		return c.TemplateScripts
	}
	return path.Join(c.MeshHostRoot, "scripts")
}
