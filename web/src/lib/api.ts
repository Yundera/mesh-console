// Thin fetch wrapper. Every request goes through the AppShield gate with the
// session cookie; the custom header on writes is what the server's CSRF guard
// checks (a cross-site page cannot set it without a preflight we never answer).

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public body?: unknown,
  ) {
    super(message)
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (method !== 'GET') {
    headers['X-Mesh-Console'] = '1'
    headers['Content-Type'] = 'application/json'
  }
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text()
  let data: unknown = undefined
  try {
    data = text ? JSON.parse(text) : undefined
  } catch {
    // The gate answers an expired session with a login page, not JSON.
  }
  if (!res.ok) {
    const msg = (data as { error?: string } | undefined)?.error ?? `${res.status} ${res.statusText}`
    throw new ApiError(res.status, msg, data)
  }
  if (data === undefined) {
    throw new ApiError(res.status, 'Unexpected response — your session may have expired. Reload the page.')
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body ?? {}),
}

// ---- response shapes (mirror internal/server/handlers.go) ----------------

export interface Overview {
  domain: string
  domainName: string
  serverDomain: string
  publicIp: string
  publicIpv4: string
  publicIpv6: string
  ipDash: string
  email: string
  links: { root: string; maison: string; dashboard?: string; sslip?: string; nip?: string }
  backendError: string
}

export interface Route {
  ip: string
  port: number
  priority: number
  source: string
  scheme?: string
  targetScheme?: string
  type?: string
  domain?: string
}

export type Mode = 'direct' | 'tunnel' | 'other' | 'offline'

export interface Pick {
  mode: Mode
  route?: Route
}

export interface Routing {
  state: { mode: Mode; gateway: Pick; worker: Pick; hasAgent: boolean; hasTunnel: boolean } | null
  routes?: Route[]
  routesTtl?: number
  lastSeenOnline?: string | null
  backendError?: string
  tunnel?: { handshakes: { interface: string; peer: string; last: string; never: boolean }[] | null; error?: string }
  cert?: { subject: string; dnsNames: string[]; notBefore?: string; notAfter: string; issuer: string }
  certError?: string
  rootDomain?: { status: number; error?: string; catchall: boolean }
}

export interface StackContainer {
  name: string
  project: string
  service: string
  image: string
  state: string
  health: string
  status: string
  declared?: string
  drift: boolean
}

export interface UpdateInfo {
  updateUrl: string
  autoUpdate: boolean
  cron: string
  windowsMode: boolean
  installed: { url: string; commit: string | null; synced_at: string } | null
  installedError?: string
  templateSyncedAt: string | null
  repo?: string
  latest: { commit: string; date?: string; message?: string; checkedAt: string } | null
  latestError?: string
  state: 'up-to-date' | 'outdated' | 'unknown'
}

export interface Step {
  name: string
  status: 'running' | 'success' | 'failed'
  started: string
  durationS?: number
  exitCode?: number
  lastOutput?: string
}

export interface Run {
  started: string
  ended?: string
  status: 'incomplete' | 'success' | 'failed' | 'interrupted'
  steps: Step[] | null
  lastLine: string
}

export interface SelfCheck {
  runs?: Run[] | null
  running?: boolean
  runnerBusy?: boolean
  log?: string[]
  error?: string
}

export interface DomainInfo {
  domain: string
  ipDash: string
  publicIp: string
  defaultHost: string
  defaultPort: string
  network: string
  editable: boolean
  /** Why the editor is read-only, when it is. */
  editBlocked?: string
  candidates?: { name: string; project?: string; state: string; ports: number[] | null; onPcs: boolean }[]
  candidatesError?: string
}

export type Level = 'ok' | 'info' | 'warn' | 'bad' | 'unknown'

export interface Tile {
  level: Level
  label: string
  detail?: string
  at?: string
  link: string
}

export interface Issue {
  level: Level
  area: string
  message: string
  link: string
}

export interface Status extends Overview {
  summary: {
    level: Level
    tiles: Record<'reachability' | 'updates' | 'services' | 'email', Tile>
    issues: Issue[]
  }
}

export interface MailCounts {
  sent: number
  failed: number
  skipped: number
  rateLimited: number
}

export interface MailEvent {
  time: string
  app: string
  from?: string
  to: string
  status: 'sent' | 'skipped' | 'failed' | 'rate_limited'
  error?: string
}

export interface MailStats {
  version: string
  since?: string | null
  retentionDays: number
  totals: { h24: MailCounts; d7: MailCounts }
  apps: (MailCounts & { app: string; from?: string; last?: string })[]
  recent: MailEvent[]
  persistent: boolean
}

export interface MailInfo {
  domainName: string
  serverDomain: string
  accountEmail: string
  limitPerHour: number
  setup: { host: string; port: number; tls: boolean; auth: boolean }
  relay?: string
  relayImage?: string
  relayState?: string
  stats?: MailStats
  statsError?: string
}

export type CertStatus = 'letsencrypt' | 'fallback' | 'unreachable'
export type Renewal = 'ok' | 'overdue' | 'expired'

export interface CertRow {
  domain: string
  sources: string[]
  status: CertStatus
  issuer?: string
  notBefore?: string
  notAfter?: string
  renewal?: Renewal
  error?: string
  reason?: string
  reasonDetail?: string
}

export interface Certificates {
  snapshotAt: string
  gateway?: { subject: string; dnsNames: string[]; notBefore: string; notAfter: string; issuer: string; renewal: Renewal }
  gatewayError?: string
  certs?: CertRow[]
  error?: string
  logError?: string
}

// ---- migration (internal/server/migration.go; status.json is written by the
// template's tools/migrate.sh and passed through verbatim) -------------------

export type MigrationPhase =
  | 'starting'
  | 'running'
  | 'rolling_back'
  | 'done'
  | 'failed'
  | 'rolled_back'
  | 'cancelled'

export interface MigrationStep {
  key: string
  label: string
  /** Who serves the apps while the step runs. */
  group: 'source' | 'down' | 'target'
  status: 'pending' | 'running' | 'success' | 'failed' | 'skipped'
  startedAt: string | null
  finishedAt: string | null
  message: string | null
}

export interface MigrationStatus {
  version: number
  id: string
  phase: MigrationPhase
  startedAt: string | null
  finishedAt: string | null
  source: { ip: string | null; domain: string | null }
  target: { host: string; user: string }
  steps: MigrationStep[]
  copy: { bytes: number; percent: number; rate: string | null; eta: string | null }
  error: string | null
  updatedAt: string
}

export interface Migration {
  available: boolean
  reason?: string
  key?: string
  status?: MigrationStatus
  /** On a box that was migrated onto: the run that brought it here. */
  arrived?: MigrationStatus
  log?: string[]
  /** MESH_ROUTING_HOLD: migrating:<id> or retired:<ip>. */
  hold?: string
}

export interface PreflightCheck {
  name: string
  status: 'ok' | 'warn' | 'fail'
  message: string
}

export interface Preflight {
  ok: boolean
  target: string
  checks: PreflightCheck[]
}
