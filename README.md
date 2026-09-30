# mesh-console

The local status and control page of a FOSS mesh box (installed from
[mesh-router-template-root](https://github.com/Yundera/mesh-router-template-root)).
Served at `mesh-console-${DOMAIN}`, behind an AppShield gate, admins only.

| Page | Shows | Acts |
|---|---|---|
| **Overview** | Domain, public IP, one verdict (**All good / Needs attention / Not working**) and four tiles — internet access, updates, services, email — with plain-language issues. Computed server-side by `internal/status` (`GET /api/status`) | — |
| **Email** | Where app mail goes and the address it is sent as (`<app>.<domainName>@<serverDomain>`); SMTP settings for apps; activity (sent / failed / not delivered, per app, recent) read from the mail relay | **Send a test email** — always to the account `EMAIL`, at most one a minute |
| **Update** | Installed template commit vs latest on the `UPDATE_URL` branch; auto-update / cron settings; last self-check runs step by step; raw log | **Update now** = run the self-check |
| **Domain** | Domain, sslip.io / nip.io fallbacks, root-domain default app, custom-domain DNS help | Change the default app (container + port) |
| **Certificates** | The mesh certificate (box domain + nip.io) and, for every sslip.io address Caddy serves (running containers' `caddy`/`caddy_N` labels + the root sslip.io block), the certificate it actually presents — Let's Encrypt, fallback (internal CA) or none — from a TLS handshake with Caddy over `pcs`. Expiry is judged against each cert's own lifetime. For a non-Let's-Encrypt address, the reason is read from Caddy's last 14 days of log. On demand only (`GET /api/certificates`), never polled | **Check again** |
| **Diagnostics** | Links (incl. sslip.io); routing state **Direct / Tunnel / Offline** with the routes the backend holds; tunnel handshake age; mesh certificate expiry; root-domain probe; platform containers and image drift | — |

## How it works

```
browser ─► mesh-router-caddy ─► mesh-console (AppShield gate, OIDC_REQUIRED_GROUPS=admins)
                                     │  X-AppShield-Assertion (HS256)
                                     ▼
                               mesh-console-app (this image)
                     ├─ reads  /mesh  = ${DATA_ROOT}/AppData/mesh, read-only
                     ├─ docker socket: list / inspect / exec `wg show` / exec `mail-gateway stats` / run the runner
                     ├─ SMTP: smtp:587 (the test email only)
                     ├─ HTTPS: mesh-router-backend  GET /router/api/{domain,resolve/v2,version}
                     └─ HTTPS: GitHub API (latest template commit, cached 1h)
```

- **Auth.** Every `/api/*` route except `/api/health` requires a valid
  `X-AppShield-Assertion` (issuer `appshield`, audience `mesh-console`, signed with
  `IDENTITY_ASSERTION_SECRET`) carrying the `admins` (or `admin`) group. Plain
  `X-Auth-Request-*` headers are ignored: any container on `pcs` can reach the app
  directly. No secret configured → every request is refused. State-changing requests
  also need an `X-Mesh-Console: 1` header (CSRF).
- **Host actions.** There are exactly two (`internal/hostverb`), and both call the
  template's own scripts, from its scripts directory (`TEMPLATE_SCRIPTS`): run
  `self-check.sh`, and `tools/set-default-app.sh <host> <port>`. The second is a
  **contract** both templates implement: the template knows where the setting is stored
  and what to recreate (the mesh stack's Caddy and the auth stack's registrar), the console
  only knows the path. Exit 75 means a self-check holds the lock (→ HTTP 409). A template
  without the tool gets a read-only editor that says why. Each runs as a
  one-shot container `mesh-console-runner` created from this same image with
  `--privileged --pid=host` and `nsenter -t 1 -m -u -i -n -p`, i.e. in the host's
  namespaces. Fixed argv, user input only in validated positional arguments, at most one
  at a time. The runner is outside any compose project so it survives the self-check
  recreating the console. No SSH, no sudoers — a FOSS box has neither set up for us.
- **Routing state** is inferred from the backend's `resolve/v2` answer the way the
  gateways pick (lowest priority wins, no failover; the CF worker only uses domain
  routes). It describes the registry, not observed traffic.
- **Mail activity** comes from `docker exec smtp /app/mail-gateway stats` (mail-gateway
  ≥ 1.1.0 keeps counters on its `/data` volume), so the relay exposes no stats port on
  `pcs`. An older relay shows "not reported" and nothing else breaks. The test email is
  plain SMTP to the relay; its recipient is never taken from the request.
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
| `MESH_HOST_ROOT` | `/DATA/AppData/mesh` | `MESH_DIR` as the host sees it |
| `TEMPLATE_SCRIPTS` | `$MESH_HOST_ROOT/scripts` | The template's scripts directory, as the host sees it — the host actions run `self-check.sh` and `tools/set-default-app.sh` from here. A Yundera PCS: `/DATA/AppData/yundera/template/scripts` |
| `LOG_FILE` | `$MESH_DIR/log/mesh.log` | The self-check log as mounted here. A Yundera PCS: `/mesh/log/yundera.log` |
| `DEFAULT_APP_EDIT` | on | `false` turns the default-app editor off. It is also off, with the reason shown, when the template has no `tools/set-default-app.sh` |
| `PLATFORM_PROJECTS` | `mesh,maison,mesh-console` | Compose projects listed as platform containers |
| `SELF_CHECK_SCRIPT` | — | Deprecated override of `$TEMPLATE_SCRIPTS/self-check.sh` |
| `SELF_CONTAINER` | `mesh-console-app` | Used to find the image the runner is created from |
| `RUNNER_IMAGE` | — | Override that lookup |
| `TUNNEL_CONTAINER` | `mesh-router-tunnel` | Where `wg show` runs |
| `CADDY_HOST` | `mesh-router-caddy` | Caddy's container (= hostname on `pcs`): target of the root-domain probe and the certificate handshakes, and whose log explains failed issuances |
| `MAIL_CONTAINER` | `smtp` | The mail relay whose activity the Email page shows |
| `SMTP_ADDR` | `smtp:587` | Where the test email is sent |
| `TZ` | host `/etc/localtime` | Timezone the self-check log (written in host local time) is parsed in. The template bind-mounts `/etc/localtime` instead of setting it |
| `LISTEN_ADDR` | `:8080` | |
| `MESH_CONSOLE_ENV`, `DEV_IDENTITY` | `production`, — | `DEV_IDENTITY=<name>` skips auth, **only** when `MESH_CONSOLE_ENV=development` and no secret is set |

The template's `docker-compose.yml` (the `mesh-console` and `mesh-console-app` services of
the mesh stack) is the reference deployment.

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
`main` and on `v*` tags. Bump by tagging (`v1.0.1` → `:1.0.1`), then move the pin in the
template's `docker-compose.yml`.
