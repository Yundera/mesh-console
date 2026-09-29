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
  cert?: { subject: string; dnsNames: string[]; notAfter: string; issuer: string }
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
  candidates?: { name: string; project?: string; state: string; ports: number[] | null; onPcs: boolean }[]
  candidatesError?: string
}
