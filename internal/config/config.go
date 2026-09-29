// Package config reads mesh-console's settings from the environment.
package config

import (
	"os"
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
	}
}

func def(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}
