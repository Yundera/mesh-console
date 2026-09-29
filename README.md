# mesh-console

The local status and control page of a FOSS mesh box (installed from
[mesh-router-template-root](https://github.com/Yundera/mesh-router-template-root)).
Served at `mesh-console-${DOMAIN}`, behind an AppShield gate, admins only.

| Page | Shows | Acts |
|---|---|---|
| **Overview** | Domain, public IP(s), links (nsl.sh dashboard, Maison, root domain, sslip.io); routing state **Direct / Tunnel / Offline** with the routes the backend holds; tunnel handshake age; mesh certificate expiry; root-domain probe; platform containers and image drift | — |
| **Update** | Installed template commit vs latest on the `UPDATE_URL` branch; auto-update / cron settings; last self-check runs step by step; raw log | **Update now** = run the self-check |
| **Domain** | Domain, sslip.io / nip.io fallbacks, root-domain default app, custom-domain DNS help | Change the default app (container + port) |

## How it works

```
browser ─► mesh-router-caddy ─► mesh-console (AppShield gate, OIDC_REQUIRED_GROUPS=admins)
                                     │  X-AppShield-Assertion (HS256)
                                     ▼
                               mesh-console-app (this image)
                     ├─ reads  /mesh  = ${DATA_ROOT}/AppData/mesh, read-only
                     ├─ docker socket: list / inspect / exec `wg show` / run the runner
                     ├─ HTTPS: mesh-router-backend  GET /router/api/{domain,resolve/v2,version}
                     └─ HTTPS: GitHub API (latest template commit, cached 1h)
```

- **Auth.** Every `/api/*` route except `/api/health` requires a valid
  `X-AppShield-Assertion` (issuer `appshield`, audience `mesh-console`, signed with
  `IDENTITY_ASSERTION_SECRET`) carrying the `admins` (or `admin`) group. Plain
  `X-Auth-Request-*` headers are ignored: any container on `pcs` can reach the app
  directly. No secret configured → every request is refused. State-changing requests
  also need an `X-Mesh-Console: 1` header (CSRF).
- **Host actions.** There are exactly two (`internal/hostverb`): run
  `scripts/self-check.sh`, and set `DEFAULT_SERVICE_HOST`/`PORT` through the template's
  `env-file-manager.sh` then `docker compose up -d` the mesh stack. Each runs as a
  one-shot container `mesh-console-runner` created from this same image with
  `--privileged --pid=host` and `nsenter -t 1 -m -u -i -n -p`, i.e. in the host's
  namespaces. Fixed argv, user input only in validated positional arguments, at most one
  at a time. The runner is outside any compose project so it survives the self-check
  recreating the console. No SSH, no sudoers — a FOSS box has neither set up for us.
- **Routing state** is inferred from the backend's `resolve/v2` answer the way the
  gateways pick (lowest priority wins, no failover; the CF worker only uses domain
  routes). It describes the registry, not observed traffic.
- **Template version** comes from `template/.revision.json`, written by
  `ensure-template-sync.sh` after each successful sync (`{url, commit, synced_at}`; the
  commit is read from the tarball's pax header). Boxes that have not synced since that
  change show "unknown".

## Configuration

| Env | Default | |
|---|---|---|
| `IDENTITY_ASSERTION_SECRET` | — | Required. Same value as the gate's |
| `IDENTITY_ASSERTION_AUDIENCE` | `mesh-console` | Must equal the gate's APP_NAME (its hostname) |
| `MESH_DIR` | `/mesh` | Mesh root as mounted here (read-only) |
| `MESH_HOST_ROOT` | `/DATA/AppData/mesh` | Mesh root as the host sees it — host actions use this |
| `SELF_CONTAINER` | `mesh-console-app` | Used to find the image the runner is created from |
| `RUNNER_IMAGE` | — | Override that lookup |
| `TUNNEL_CONTAINER` | `mesh-router-tunnel` | Where `wg show` runs |
| `CADDY_HOST` | `mesh-router-caddy` | Target of the root-domain probe |
| `TZ` | `UTC` | Host timezone; the self-check log is written in local time |
| `LISTEN_ADDR` | `:8080` | |
| `MESH_CONSOLE_ENV`, `DEV_IDENTITY` | `production`, — | `DEV_IDENTITY=<name>` skips auth, **only** when `MESH_CONSOLE_ENV=development` and no secret is set |

The template's `stacks/mesh-console/docker-compose.yml` is the reference deployment.

## Development

No local Go or Node is needed — build and test in containers (bind-mount the **host** path):

```bash
# Go: vet + tests
docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c 'go vet ./... && go test ./...'

# UI: type-check + build into internal/ui/dist (embedded by go build)
docker run --rm -v "$PWD":/src -w /src/web node:22 sh -c 'npm install && npm run check && npm run build'

# Image
docker build -t mesh-console:dev --build-arg BUILD_VERSION=dev .

# Run against a fixture mesh dir, no gate
docker run --rm -p 18080:8080 -v "$PWD/dev/mesh":/mesh:ro -v /var/run/docker.sock:/var/run/docker.sock \
  -e DEV_IDENTITY=dev -e MESH_CONSOLE_ENV=development -e RUNNER_IMAGE=mesh-console:dev \
  -e MESH_HOST_ROOT=/nonexistent mesh-console:dev
```

`dev/mesh/` (gitignored) needs at least a `.env` with `DOMAIN` and `PROVIDER_STR`;
add `log/mesh.log`, `template/.revision.json`, `data/certs/cert.pem` and
`docker-compose.yml` to light up the rest. Keep `MESH_HOST_ROOT` pointing at a
nonexistent path locally, so the host actions fail harmlessly instead of acting on the
machine you develop on.

## Release

GitHub Actions publishes `ghcr.io/yundera/mesh-console` (amd64 + arm64) on pushes to
`main` and on `v*` tags. Bump by tagging (`v0.1.0` → `:0.1.0`), then move the pin in the
template's `stacks/mesh-console/docker-compose.yml`.
