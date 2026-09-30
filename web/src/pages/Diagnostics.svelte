<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import { api, type Overview, type Routing, type StackContainer, type Mode } from '../lib/api'
  import { ago, dateTime } from '../lib/format'

  let overview = $state<Overview | null>(null)
  let routing = $state<Routing | null>(null)
  let stack = $state<StackContainer[] | null>(null)
  let errors = $state<{ overview?: string; routing?: string; stack?: string }>({})
  let loadingRouting = $state(false)

  async function loadOverview() {
    try {
      overview = await api.get<Overview>('/api/overview')
      errors.overview = undefined
    } catch (e) {
      errors.overview = (e as Error).message
    }
  }
  async function loadRouting() {
    loadingRouting = true
    try {
      routing = await api.get<Routing>('/api/routing')
      errors.routing = undefined
    } catch (e) {
      errors.routing = (e as Error).message
    } finally {
      loadingRouting = false
    }
  }
  async function loadStack() {
    try {
      stack = (await api.get<{ containers: StackContainer[] | null }>('/api/stack')).containers ?? []
      errors.stack = undefined
    } catch (e) {
      errors.stack = (e as Error).message
    }
  }

  $effect(() => {
    loadOverview()
    loadRouting()
    loadStack()
    const t = setInterval(() => {
      loadRouting()
      loadStack()
    }, 30_000)
    return () => clearInterval(t)
  })

  const modeLabel: Record<Mode, string> = {
    direct: 'Direct',
    tunnel: 'Tunnel',
    other: 'Other',
    offline: 'Offline',
  }
  const modeTone = (m: Mode) => (m === 'direct' ? 'ok' : m === 'tunnel' ? 'info' : m === 'offline' ? 'bad' : 'warn')

  function modeExplain(r: Routing): string {
    const s = r.state
    if (!s) return ''
    if (s.mode === 'direct') return 'The gateway connects straight to this box’s public IP (mesh-router-agent).'
    if (s.mode === 'tunnel')
      return s.hasAgent
        ? 'A direct route is registered but ranks below the tunnel.'
        : 'No direct route is registered, so traffic goes through the WireGuard tunnel. This is normal behind NAT; on a box with a public IP it usually means the gateway could not validate the direct route.'
    if (s.mode === 'offline')
      return 'The backend holds no live route for this box. Public URLs will not load until the agent or the tunnel registers again (every few minutes).'
    return 'A route from an unexpected source ranks first.'
  }

  // The agent's certificates last days and renew at half-life, so warn only
  // once a renewal is overdue (under a quarter of the lifetime left).
  function certTone(notBefore: string | undefined, notAfter: string) {
    const end = new Date(notAfter).getTime()
    const left = end - Date.now()
    const life = notBefore ? end - new Date(notBefore).getTime() : 28 * 86_400_000
    return left < 0 ? 'bad' : left < life / 4 ? 'warn' : 'ok'
  }

  function handshakeTone(last: string, never: boolean) {
    if (never) return 'bad'
    const age = (Date.now() - new Date(last).getTime()) / 1000
    // wg keepalive re-handshakes every ~2 min; the tunnel restarts a link after 5.
    return age < 180 ? 'ok' : age < 300 ? 'warn' : 'bad'
  }

  function rootTone(status: number) {
    return status >= 200 && status < 400 ? 'ok' : status >= 500 || status === 0 ? 'bad' : 'warn'
  }
</script>

<div class="grid">
  <Card title="This box">
    {#if errors.overview}
      <p class="error">{errors.overview}</p>
    {:else if !overview}
      <p class="subtle">Loading…</p>
    {:else}
      <dl class="kv">
        <dt>Domain</dt>
        <dd><a href={overview.links.root} target="_blank" rel="noopener">{overview.domain}</a></dd>
        <dt>Public IP</dt>
        <dd class="mono">{overview.publicIp || '—'}</dd>
        {#if overview.publicIpv4 && overview.publicIpv4 !== overview.publicIp}
          <dt>IPv4</dt>
          <dd class="mono">{overview.publicIpv4}</dd>
        {/if}
        {#if overview.publicIpv6 && overview.publicIpv6 !== overview.publicIp}
          <dt>IPv6</dt>
          <dd class="mono">{overview.publicIpv6}</dd>
        {/if}
        {#if overview.email}
          <dt>Email</dt>
          <dd>{overview.email}</dd>
        {/if}
      </dl>
      {#if overview.backendError}
        <p class="muted small">Backend unreachable ({overview.backendError}); names derived from the local .env.</p>
      {/if}
    {/if}
  </Card>

  <Card title="Links">
    {#if overview}
      <ul class="links">
        {#if overview.links.dashboard}
          <li>
            <a href={overview.links.dashboard} target="_blank" rel="noopener">{overview.serverDomain} dashboard</a>
            <span class="subtle">— your account, domain and devices</span>
          </li>
        {/if}
        <li><a href={overview.links.maison} target="_blank" rel="noopener">Maison</a> <span class="subtle">— apps</span></li>
        <li><a href={overview.links.root} target="_blank" rel="noopener">{overview.domain}</a> <span class="subtle">— root domain</span></li>
        {#if overview.links.sslip}
          <li>
            <a href={overview.links.sslip} target="_blank" rel="noopener">{overview.ipDash}.sslip.io</a>
            <span class="subtle">— direct, bypasses the gateway</span>
          </li>
        {/if}
      </ul>
    {/if}
  </Card>

  <div class="wide">
    <Card title="Routing">
      {#snippet actions()}
        <button onclick={loadRouting} disabled={loadingRouting}>{loadingRouting ? 'Checking…' : 'Refresh'}</button>
      {/snippet}
      {#if errors.routing}
        <p class="error">{errors.routing}</p>
      {:else if !routing}
        <p class="subtle">Loading…</p>
      {:else}
        {#if routing.state}
          <div class="mode">
            <Pill tone={modeTone(routing.state.mode)}>{modeLabel[routing.state.mode]}</Pill>
            <span>{modeExplain(routing)}</span>
          </div>
          {#if routing.state.worker.mode !== routing.state.gateway.mode}
            <p class="muted small">
              Cloudflare worker path: <strong>{modeLabel[routing.state.worker.mode]}</strong> — it only uses domain routes,
              {routing.state.worker.mode === 'offline' ? 'and none is registered, so it falls back to the gateway.' : 'so it picks differently.'}
            </p>
          {/if}
          <p class="subtle small">
            As the gateways see it: the lowest-priority route wins, there is no failover.
            Routes expire in {routing.routesTtl !== undefined && routing.routesTtl > 0 ? `${routing.routesTtl}s` : '—'} unless refreshed; last registration {ago(routing.lastSeenOnline)}.
          </p>
          {#if routing.routes && routing.routes.length}
            <div class="table-wrap">
              <table>
                <thead><tr><th>Source</th><th>Priority</th><th>Scheme</th><th>Target</th></tr></thead>
                <tbody>
                  {#each routing.routes as r, i (i)}
                    <tr class:active={routing.state.gateway.route && r.source === routing.state.gateway.route.source && r.priority === routing.state.gateway.route.priority && (r.scheme ?? 'https') === (routing.state.gateway.route.scheme ?? 'https')}>
                      <td>{r.source === 'agent' ? 'agent (direct)' : r.source}</td>
                      <td>{r.priority || '—'}</td>
                      <td>{r.scheme ?? 'https'}</td>
                      <td class="mono">{r.domain || r.ip}:{r.port}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        {:else}
          <p class="error">Backend unreachable: {routing.backendError}</p>
        {/if}

        <h3>Local checks</h3>
        <dl class="kv">
          <dt>Root domain</dt>
          <dd>
            {#if routing.rootDomain}
              {#if routing.rootDomain.error}
                <Pill tone="bad">error</Pill> <span class="small">{routing.rootDomain.error}</span>
              {:else}
                <Pill tone={rootTone(routing.rootDomain.status)}>HTTP {routing.rootDomain.status}</Pill>
                <span class="subtle small">Caddy → default app{routing.rootDomain.catchall ? ' (catch-all)' : ''}</span>
              {/if}
            {:else}—{/if}
          </dd>
          <dt>Tunnel</dt>
          <dd>
            {#if routing.tunnel?.error}
              <Pill tone="warn">unavailable</Pill> <span class="small subtle">{routing.tunnel.error}</span>
            {:else if routing.tunnel?.handshakes?.length}
              {#each routing.tunnel.handshakes as h (h.interface + h.peer)}
                <div>
                  <Pill tone={handshakeTone(h.last, h.never)}>{h.never ? 'no handshake' : `handshake ${ago(h.last)}`}</Pill>
                  <span class="mono subtle">{h.interface}</span>
                </div>
              {/each}
            {:else}
              <Pill tone="neutral">no WireGuard link</Pill>
            {/if}
          </dd>
          <dt>Mesh certificate</dt>
          <dd>
            {#if routing.cert}
              <Pill tone={certTone(routing.cert.notBefore, routing.cert.notAfter)}>expires {ago(routing.cert.notAfter)}</Pill>
              <span class="subtle small">{dateTime(routing.cert.notAfter)} · {routing.cert.dnsNames?.length ?? 0} names</span>
            {:else}
              <span class="small error">{routing.certError}</span>
            {/if}
          </dd>
        </dl>
      {/if}
    </Card>
  </div>

  <div class="wide">
    <Card title="Platform containers">
      {#if errors.stack}
        <p class="error">{errors.stack}</p>
      {:else if !stack}
        <p class="subtle">Loading…</p>
      {:else if stack.length === 0}
        <p class="subtle">No containers from the mesh, maison or mesh-console stacks were found.</p>
      {:else}
        <div class="table-wrap">
          <table>
            <thead><tr><th>Container</th><th>State</th><th>Image</th></tr></thead>
            <tbody>
              {#each stack as c (c.name)}
                <tr>
                  <td>{c.name} <span class="subtle small">{c.project}</span></td>
                  <td>
                    <Pill tone={c.state !== 'running' ? 'bad' : c.health === 'unhealthy' ? 'bad' : c.health === 'starting' ? 'warn' : 'ok'}>
                      {c.health || c.state}
                    </Pill>
                    <span class="subtle small">{c.status}</span>
                  </td>
                  <td class="mono">
                    {c.image}
                    {#if c.drift}<div><Pill tone="warn">compose pins {c.declared}</Pill></div>{/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </Card>
  </div>
</div>

<style>
  .grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 1rem;
  }
  .wide {
    grid-column: 1 / -1;
    min-width: 0;
  }
  @media (max-width: 760px) {
    .grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .links {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    gap: 0.45rem;
  }
  .mode {
    display: flex;
    gap: 0.6rem;
    align-items: baseline;
    margin-bottom: 0.4rem;
  }
  .small {
    font-size: 0.8rem;
  }
  h3 {
    font-size: 0.85rem;
    margin: 1.1rem 0 0.6rem;
  }
  tr.active td {
    background: var(--ok-bg);
  }
</style>
