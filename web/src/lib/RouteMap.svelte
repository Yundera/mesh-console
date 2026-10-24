<script lang="ts" module>
  // What clicking a grey part does, with the two switches the template ships today
  // (tools/set-routing-mode.sh and tools/set-dns-force.sh).
  export type ActionKind = 'tunnel' | 'auto' | 'dns-gateway' | 'dns-auto'
  export interface Action {
    kind: ActionKind
    title: string
    explain: string
    // A second switch the action can flip too, ticked or not by default.
    also?: { label: string; checked: boolean }
  }
</script>

<script lang="ts">
  import Pill from './Pill.svelte'
  import type { BoxCert, Lane, Mode, Overview, Routing, Trace } from './api'
  import { ago } from './format'

  let {
    routing,
    overview,
    busy,
    onaction,
  }: {
    routing: Routing
    overview: Overview | null
    busy: boolean
    onaction: (a: Action, also: boolean) => void
  } = $props()

  type LaneId = Exclude<Lane, 'unknown'>
  type Entry = 'box' | 'cloudflare' | 'gateway'
  type State = 'observed' | 'inferred' | 'idle' | 'unavailable'

  const domain = $derived(overview?.domain ?? routing.trace?.[0]?.host ?? '')
  const traces = $derived(routing.trace ?? [])
  // A trace that says nothing about the path (no DNS answer, nothing recognised)
  // falls back to what the registry implies.
  const usable = (t: Trace) => t.lane !== 'unknown' && t.entry !== 'unknown'

  function gatewayLane(m: Mode): LaneId | null {
    return m === 'direct' ? 'gateway-agent' : m === 'tunnel' ? 'gateway-tunnel' : null
  }

  const zoneLive = $derived(!!routing.dns?.delegated && !!routing.dns.live)
  const inZone = (host: string) => host === domain || host.endsWith('.' + domain)

  // What the backend's registry implies for a name. Names in the box's own zone
  // follow the nameserver's answer when the zone is delegated; everything else is
  // read as going through the Cloudflare worker, which also honours the owner's
  // "through the gateway" choice. The trace is the truth; this is the fallback.
  function inferLane(host: string): LaneId | null {
    const s = routing.state
    if (!s) return null
    const d = routing.dns
    if (inZone(host) && zoneLive) return d!.answer === 'direct' ? 'box-direct' : gatewayLane(s.gateway.mode)
    if (d?.force === 'gateway') return gatewayLane(s.gateway.mode)
    if (s.worker.mode === 'direct') return 'edge-direct'
    return gatewayLane(s.gateway.mode)
  }
  function inferEntry(host: string, lane: LaneId): Entry {
    if (lane === 'box-direct') return 'box'
    if (lane === 'edge-direct') return 'cloudflare'
    return inZone(host) && zoneLive ? 'gateway' : 'cloudflare'
  }

  const dnsDirect = $derived(
    !!routing.dns?.editable && routing.dns.force === 'auto' && routing.dns.answer === 'direct',
  )
  const forceEditable = $derived(!!routing.force?.editable)
  const tunnelForced = $derived(routing.force?.mode === 'tunnel')

  function unavailable(id: LaneId): string | undefined {
    const s = routing.state
    const d = routing.dns
    switch (id) {
      case 'box-direct':
        if (!d?.delegated) return 'your zone is not delegated to the mesh nameservers'
        if (!d.live) return 'the delegation is not live yet'
        if (routing.boxCert && routing.boxCert.kind !== 'letsencrypt') return 'the box has no publicly trusted certificate yet'
        return undefined
      case 'edge-direct':
        if (!routing.routes?.some((r) => r.source === 'agent' && r.type === 'domain' && r.domain))
          return 'no direct route the Cloudflare worker can use'
        return undefined
      case 'gateway-agent':
        return s && !s.hasAgent ? 'no direct route registered (behind NAT, or the backend could not validate it)' : undefined
      case 'gateway-tunnel':
        return s && !s.hasTunnel ? 'no tunnel route registered' : undefined
    }
  }

  const dnsGateway: Action = {
    kind: 'dns-gateway',
    title: 'Answer the gateway',
    explain:
      'Your names will answer the gateway instead of this box, and the gateway picks the route. Browsers follow within a minute.',
  }

  function actionFor(id: LaneId): Action | undefined {
    const d = routing.dns
    switch (id) {
      case 'gateway-tunnel':
        if (!routing.state?.hasTunnel || !forceEditable) return undefined
        if (!tunnelForced)
          return {
            kind: 'tunnel',
            title: 'Force the tunnel',
            explain:
              'Traffic goes through the WireGuard tunnel even when a direct connection is available. It is on trial: it switches back to automatic after 5 minutes unless you keep it.' +
              (dnsDirect
                ? ' Your names answer this box directly, so they only reach the tunnel once they answer the gateway. The trial does not undo that part; the gateway always has a route, so it is safe to leave.'
                : ''),
            also: dnsDirect ? { label: 'Also make your names answer the gateway', checked: true } : undefined,
          }
        // Forced, but the names still answer the box directly.
        return dnsDirect ? { ...dnsGateway, explain: dnsGateway.explain + ' The tunnel is forced, so it will take them.' } : undefined
      case 'gateway-agent':
        if (!routing.state?.hasAgent) return undefined
        if (tunnelForced && forceEditable)
          return {
            kind: 'auto',
            title: 'Back to automatic',
            explain: 'The direct connection wins again: the gateway goes straight to this box’s public IP.',
            also:
              d?.editable && d.force === 'gateway'
                ? { label: 'Also let your names answer this box directly again', checked: false }
                : undefined,
          }
        return dnsDirect ? dnsGateway : undefined
      case 'box-direct':
        return d?.editable && d.force === 'gateway'
          ? {
              kind: 'dns-auto',
              title: 'Answer this box directly',
              explain:
                'Back to automatic: your names answer this box directly when it serves them with a trusted certificate, the gateway otherwise.',
            }
          : undefined
      case 'edge-direct':
        return undefined
    }
  }

  // ---- the graph -------------------------------------------------------------
  // Columns follow the traffic; rows separate the ways through. The box spans
  // rows 0-3 and every way in arrives on its own row.

  type NodeId = 'in' | 'dns' | 'worker' | 'gateway' | 'agent' | 'tunnel' | 'box' | 'out'
  type EdgeId =
    | 'in-dns'
    | 'dns-box'
    | 'dns-worker'
    | 'dns-gateway'
    | 'worker-box'
    | 'worker-gateway'
    | 'gateway-agent'
    | 'gateway-tunnel'
    | 'agent-box'
    | 'tunnel-box'
    | 'box-out'

  const NODES: { id: NodeId; col: number; row: number; label: string; sub: string }[] = [
    { id: 'in', col: 0, row: 1.5, label: 'Internet', sub: 'visitors' },
    { id: 'dns', col: 1, row: 1.5, label: 'DNS', sub: 'your names' },
    { id: 'worker', col: 2, row: 1, label: 'Cloudflare', sub: 'worker' },
    { id: 'gateway', col: 3, row: 2, label: 'Gateway', sub: 'mesh edge' },
    { id: 'agent', col: 4, row: 2, label: 'Public IP', sub: 'agent route' },
    { id: 'tunnel', col: 4, row: 3, label: 'Tunnel', sub: 'WireGuard' },
    { id: 'box', col: 5, row: 1.5, label: 'This box', sub: 'Caddy → apps' },
    { id: 'out', col: 6, row: 1.5, label: 'Internet', sub: 'outgoing' },
  ]
  // `row` is where the edge arrives on the box (its own lane), else the target's row.
  const EDGES: { id: EdgeId; from: NodeId; to: NodeId; row?: number; label?: string }[] = [
    { id: 'in-dns', from: 'in', to: 'dns' },
    { id: 'dns-box', from: 'dns', to: 'box', row: 0, label: 'answers this box' },
    { id: 'dns-worker', from: 'dns', to: 'worker' },
    { id: 'dns-gateway', from: 'dns', to: 'gateway', label: 'answers the gateway' },
    { id: 'worker-box', from: 'worker', to: 'box', row: 1, label: 'by name' },
    { id: 'worker-gateway', from: 'worker', to: 'gateway' },
    { id: 'gateway-agent', from: 'gateway', to: 'agent' },
    { id: 'gateway-tunnel', from: 'gateway', to: 'tunnel' },
    { id: 'agent-box', from: 'agent', to: 'box', row: 2 },
    { id: 'tunnel-box', from: 'tunnel', to: 'box', row: 3 },
    { id: 'box-out', from: 'box', to: 'out' },
  ]

  function pathEdges(lane: LaneId, entry: Entry): EdgeId[] {
    if (lane === 'box-direct') return ['in-dns', 'dns-box']
    if (lane === 'edge-direct') return ['in-dns', 'dns-worker', 'worker-box']
    const head: EdgeId[] = entry === 'gateway' ? ['in-dns', 'dns-gateway'] : ['in-dns', 'dns-worker', 'worker-gateway']
    return lane === 'gateway-agent' ? [...head, 'gateway-agent', 'agent-box'] : [...head, 'gateway-tunnel', 'tunnel-box']
  }

  // One entry per name: the path it took (or, unchecked, the one the registry implies).
  interface HostPath {
    host: string
    state: 'observed' | 'inferred'
    lane: LaneId
    entry: Entry
    edges: EdgeId[]
    trace?: Trace
  }
  const hostPaths = $derived.by((): HostPath[] => {
    const names = traces.length ? traces.map((t) => t.host) : domain ? [domain] : []
    const out: HostPath[] = []
    for (const host of names) {
      const t = traces.find((x) => x.host === host)
      if (t && usable(t)) {
        const lane = t.lane as LaneId
        const entry = t.entry as Entry
        out.push({ host, state: 'observed', lane, entry, edges: pathEdges(lane, entry), trace: t })
        continue
      }
      const lane = inferLane(host)
      if (lane) {
        const entry = inferEntry(host, lane)
        out.push({ host, state: 'inferred', lane, entry, edges: pathEdges(lane, entry), trace: t })
      }
    }
    return out
  })

  // Why an edge cannot carry traffic, when it cannot.
  const edgeBlocked = $derived.by((): Partial<Record<EdgeId, string>> => {
    const b: Partial<Record<EdgeId, string>> = {}
    const direct = unavailable('box-direct')
    if (direct) b['dns-box'] = direct
    if (!zoneLive) b['dns-gateway'] = unavailable('box-direct') ?? 'your zone is not delegated'
    const edge = unavailable('edge-direct')
    if (edge) b['worker-box'] = edge
    const agent = unavailable('gateway-agent')
    if (agent) b['gateway-agent'] = b['agent-box'] = agent
    const tunnel = unavailable('gateway-tunnel')
    if (tunnel) b['gateway-tunnel'] = b['tunnel-box'] = tunnel
    return b
  })

  const rank: Record<State, number> = { observed: 3, inferred: 2, idle: 1, unavailable: 0 }
  const edgeState = $derived.by((): Record<EdgeId, State> => {
    const st = {} as Record<EdgeId, State>
    for (const e of EDGES) st[e.id] = edgeBlocked[e.id] ? 'unavailable' : 'idle'
    for (const hp of hostPaths)
      for (const id of hp.edges) if (rank[hp.state] > rank[st[id]]) st[id] = hp.state
    // The way out is the one the traces themselves took.
    st['box-out'] = traces.some((t) => t.status) ? 'observed' : 'idle'
    return st
  })
  const nodeState = $derived.by((): Record<NodeId, State> => {
    const st = {} as Record<NodeId, State>
    for (const n of NODES) {
      const touching = EDGES.filter((e) => e.to === n.id || (n.id === 'in' && e.from === 'in')).map((e) => edgeState[e.id])
      st[n.id] = touching.length
        ? touching.reduce((a, b) => (rank[b] > rank[a] ? b : a), 'unavailable' as State)
        : 'idle'
    }
    st.box = hostPaths.length ? hostPaths.some((h) => h.state === 'observed') ? 'observed' : 'inferred' : 'idle'
    return st
  })

  // Clickable grey parts: the hop to switch to, and the lane its action belongs to.
  const targets = $derived.by((): Partial<Record<NodeId | EdgeId, Action>> => {
    const t: Partial<Record<NodeId | EdgeId, Action>> = {}
    const lit = (s: State) => s === 'observed' || s === 'inferred'
    const tunnel = actionFor('gateway-tunnel')
    if (tunnel && !lit(nodeState.tunnel) && nodeState.tunnel !== 'unavailable') t.tunnel = tunnel
    const agent = actionFor('gateway-agent')
    if (agent && !lit(nodeState.agent) && nodeState.agent !== 'unavailable') t.agent = agent
    if (dnsDirect && !lit(nodeState.gateway)) t.gateway = dnsGateway
    const box = actionFor('box-direct')
    if (box && !lit(edgeState['dns-box'])) t['dns-box'] = box
    return t
  })

  // ---- geometry --------------------------------------------------------------
  // Laid out along a flow axis (a) and a cross axis (c); wide containers flow left
  // to right, narrow ones top to bottom, from the same columns and rows.

  let width = $state(900)
  const vertical = $derived(width < 640)
  const G = $derived(
    vertical
      ? { step: 78, lane: 100, sa: 40, sc: 92, pad: 12 } // sa/sc: node size along/across the flow
      : { step: 140, lane: 54, sa: 104, sc: 40, pad: 12 },
  )
  const node = (id: NodeId) => NODES.find((n) => n.id === id)!
  const A = (col: number) => G.pad + G.sa / 2 + col * G.step
  const C = (row: number) => G.pad + G.sc / 2 + row * G.lane
  const xy = (a: number, c: number) => (vertical ? { x: c, y: a } : { x: a, y: c })
  const viewW = $derived(vertical ? 2 * G.pad + 3 * G.lane + G.sc : 2 * G.pad + 6 * G.step + G.sa)
  const viewH = $derived(vertical ? 2 * G.pad + 6 * G.step + G.sa : 2 * G.pad + 3 * G.lane + G.sc)

  function nodeRect(id: NodeId) {
    const n = node(id)
    if (id === 'box') {
      const p = xy(A(n.col) - G.sa / 2, C(0) - G.sc / 2)
      const along = G.sa
      const across = 3 * G.lane + G.sc
      return vertical ? { ...p, w: across, h: along } : { ...p, w: along, h: across }
    }
    const p = xy(A(n.col) - G.sa / 2, C(n.row) - G.sc / 2)
    return vertical ? { ...p, w: G.sc, h: G.sa } : { ...p, w: G.sa, h: G.sc }
  }

  function edgeGeom(e: (typeof EDGES)[number]) {
    const f = node(e.from)
    const t = node(e.to)
    const sa = A(f.col) + G.sa / 2
    const ec = C(e.row ?? t.row)
    // Fan out of the source's side toward each target, so edges leaving one node
    // do not start from a single point.
    const fan = G.sc / 2 - 7
    const sc = C(f.row) + Math.max(-fan, Math.min(fan, (ec - C(f.row)) * 0.3))
    const ea = A(t.col) - G.sa / 2 - 2
    const gap = G.step - G.sa
    const m = sa + gap
    const p = (a: number, c: number) => {
      const q = xy(a, c)
      return `${q.x},${q.y}`
    }
    const straight = ea - m > 1
    const d = straight
      ? `M${p(sa, sc)} C${p(sa + gap / 2, sc)} ${p(sa + gap / 2, ec)} ${p(m, ec)} L${p(ea, ec)}`
      : `M${p(sa, sc)} C${p(sa + gap / 2, sc)} ${p(sa + gap / 2, ec)} ${p(ea, ec)}`
    const l = straight ? xy((m + ea) / 2, ec) : xy((sa + ea) / 2, (sc + ec) / 2)
    return { d, lx: l.x, ly: l.y }
  }

  // The worker's way to the box, as it reported it (ip-dns, sslip.io, …).
  function edgeLabel(e: (typeof EDGES)[number]): string | undefined {
    if (e.id === 'worker-box') {
      const hp = hostPaths.find((h) => h.lane === 'edge-direct' && h.trace)
      const hops = hp?.trace?.hops.filter((h) => h !== 'owner-force') ?? []
      return hops.length ? hops.join(' → ') : e.label
    }
    return e.label
  }

  // ---- interaction -------------------------------------------------------------

  let selected = $state<NodeId | EdgeId | null>(null)
  let also = $state(false)
  const selectedAction = $derived(selected ? targets[selected] : undefined)

  function pick(id: NodeId | EdgeId) {
    const a = targets[id]
    if (!a || busy) return
    selected = selected === id ? null : id
    also = a.also?.checked ?? false
  }
  function key(e: KeyboardEvent, id: NodeId | EdgeId) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      pick(id)
    }
  }
  function confirm() {
    if (!selectedAction) return
    onaction(selectedAction, also)
    selected = null
  }

  // ---- words -------------------------------------------------------------------

  function pathWords(h: HostPath): string {
    const start = h.entry === 'box' ? 'DNS answers this box' : h.entry === 'gateway' ? 'DNS answers the gateway' : 'Cloudflare worker'
    switch (h.lane) {
      case 'box-direct':
        return `${start} → this box`
      case 'edge-direct': {
        const hops = h.trace?.hops.filter((x) => x !== 'owner-force') ?? []
        return `${start} → ${hops.length ? hops.join(' → ') : 'by name'} → this box`
      }
      case 'gateway-agent':
        return `${start} → gateway → public IP → this box`
      case 'gateway-tunnel':
        return `${start} → gateway → tunnel → this box`
    }
  }

  // Where the trace and the registry disagree. Expected for about a minute after a
  // switch: DNS answers live 30 s, the worker caches routes 60 s.
  const mismatches = $derived(
    hostPaths
      .filter((h) => h.state === 'observed')
      .map((h) => ({ h, want: inferLane(h.host) }))
      .filter(({ h, want }) => want && want !== h.lane),
  )
  const failed = $derived(traces.filter((t) => !usable(t) || t.error))
  const blockedList = $derived(
    (
      [
        ['Direct from DNS', edgeBlocked['dns-box']],
        ['Worker by name', edgeBlocked['worker-box']],
        ['Public IP', edgeBlocked['gateway-agent']],
        ['Tunnel', edgeBlocked['gateway-tunnel']],
      ] as [string, string | undefined][]
    ).filter(([, why]) => why),
  )

  const laneName: Record<LaneId, string> = {
    'box-direct': 'directly to this box',
    'edge-direct': 'the Cloudflare worker, by name',
    'gateway-agent': 'the gateway, to the public IP',
    'gateway-tunnel': 'the gateway, through the tunnel',
  }

  const certKind: Record<BoxCert['kind'], string> = {
    letsencrypt: 'Let’s Encrypt',
    'mesh-ca': 'Mesh CA',
    other: 'Other issuer',
    unreachable: 'No answer',
  }
  function boxCertTone(c: BoxCert) {
    if (c.kind === 'unreachable' || c.renewal === 'expired') return 'bad'
    if (c.renewal === 'overdue') return 'warn'
    return c.kind === 'letsencrypt' ? 'ok' : c.kind === 'mesh-ca' ? 'info' : 'warn'
  }
  function boxCertExplain(c: BoxCert): string {
    switch (c.kind) {
      case 'letsencrypt':
        return c.method === 'dns-01'
          ? `publicly trusted wildcard (*.${c.domain}), so browsers can reach the box directly`
          : 'publicly trusted certificate for the box domain'
      case 'mesh-ca':
        return 'signed by the mesh CA: only the gateway and the Cloudflare worker trust it'
      case 'other':
        return `issued by ${c.issuer ?? 'an unknown CA'}`
      default:
        return c.error ?? 'Caddy did not answer the handshake'
    }
  }
  const ipModeExplain: Record<string, string> = {
    egress: 'the address it connects from',
    interface: 'an address bound on this machine',
  }
</script>

{#snippet nodeBody(id: NodeId)}
  {@const r = nodeRect(id)}
  {@const n = node(id)}
  <rect class="n {nodeState[id]}" class:sel={selected === id} x={r.x} y={r.y} width={r.w} height={r.h} rx="7" />
  <text class="nl {nodeState[id]}" x={r.x + r.w / 2} y={r.y + r.h / 2 - 2} text-anchor="middle">{n.label}</text>
  <text class="ns" class:cta={!!targets[id]} x={r.x + r.w / 2} y={r.y + r.h / 2 + 11} text-anchor="middle">
    {targets[id] ? 'click to use' : n.sub}
  </text>
{/snippet}

{#snippet edgeBody(e: (typeof EDGES)[number])}
  {@const g = edgeGeom(e)}
  {@const label = edgeLabel(e)}
  <path class="e {edgeState[e.id]}" class:sel={selected === e.id} d={g.d} marker-end="url(#rm-arrow-{edgeState[e.id]})" />
  {#if label}
    <text class="el {edgeState[e.id]}" x={g.lx} y={g.ly + 4} text-anchor="middle">{label}</text>
  {/if}
{/snippet}

<div class="wrap" bind:clientWidth={width}>
  <svg
    viewBox="0 0 {viewW} {viewH}"
    width="100%"
    role="img"
    aria-label="How traffic reaches this box: lit arrows are in use, grey ones are not"
  >
    <defs>
      <pattern id="rm-hatch" width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)">
        <rect width="6" height="6" class="hatch-bg" />
        <line x1="0" y1="0" x2="0" y2="6" class="hatch-line" />
      </pattern>
      {#each ['observed', 'inferred', 'idle', 'unavailable'] as s (s)}
        <marker id="rm-arrow-{s}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
          <path d="M0,0 L10,5 L0,10 z" class="arrowhead {s}" />
        </marker>
      {/each}
    </defs>

    {#each EDGES as e (e.id)}
      {#if targets[e.id]}
        <g class="click" role="button" tabindex="0" aria-label={targets[e.id]!.title} onclick={() => pick(e.id)} onkeydown={(ev) => key(ev, e.id)}>
          <title>{targets[e.id]!.title}</title>
          {@render edgeBody(e)}
        </g>
      {:else}
        <g>
          {#if edgeBlocked[e.id]}<title>Unavailable: {edgeBlocked[e.id]}</title>{/if}
          {@render edgeBody(e)}
        </g>
      {/if}
    {/each}

    {#each NODES as n (n.id)}
      {#if targets[n.id]}
        <g class="click" role="button" tabindex="0" aria-label={targets[n.id]!.title} onclick={() => pick(n.id)} onkeydown={(ev) => key(ev, n.id)}>
          <title>{targets[n.id]!.title}</title>
          {@render nodeBody(n.id)}
        </g>
      {:else}
        <g>{@render nodeBody(n.id)}</g>
      {/if}
    {/each}
  </svg>
</div>

{#if selectedAction}
  <div class="confirm">
    <p class="small"><strong>{selectedAction.title}</strong> — {selectedAction.explain}</p>
    {#if selectedAction.also}
      <label class="small"><input type="checkbox" bind:checked={also} /> {selectedAction.also.label}</label>
    {/if}
    <div class="actions">
      <button class="primary" onclick={confirm} disabled={busy}>Confirm</button>
      <button onclick={() => (selected = null)}>Cancel</button>
    </div>
  </div>
{/if}

<ul class="paths">
  {#each hostPaths as h (h.host)}
    <li>
      <span class="dot {h.state}"></span>
      <span class="mono host">{h.host}</span>
      <span class="small">{pathWords(h)}</span>
      {#if h.state === 'observed'}
        <span class="subtle small">observed {ago(h.trace?.checkedAt)}</span>
      {:else}
        <span class="subtle small">inferred from the routes</span>
      {/if}
    </li>
  {/each}
  {#if routing.boxCert}
    <li>
      <span class="dot neutral"></span>
      <span class="small">This box serves</span>
      <Pill tone={boxCertTone(routing.boxCert)}>{certKind[routing.boxCert.kind]}</Pill>
      <span class="subtle small">
        {boxCertExplain(routing.boxCert)}{routing.boxCert.notAfter ? `, expires ${ago(routing.boxCert.notAfter)}` : ''}
      </span>
    </li>
  {/if}
  <li>
    <span class="dot neutral"></span>
    <span class="small grow">
      Outgoing traffic leaves from the box’s own address
      {#if overview?.publicIp}<span class="mono">{overview.publicIp}</span>{/if}
      {#if overview?.publicIpMode}<span class="subtle">({ipModeExplain[overview.publicIpMode] ?? overview.publicIpMode})</span>{/if};
      the tunnel carries incoming traffic only.
    </span>
  </li>
</ul>

{#if failed.length || mismatches.length}
  <ul class="notes small">
    {#each failed as t (t.host)}
      <li>
        <span class="mono">{t.host}</span>:
        {usable(t) ? 'reached, but' : 'could not be checked from the box —'}
        <span class="subtle">{t.error ?? 'no recognisable route'}</span>{usable(t) ? '' : '; showing what the routes imply.'}
      </li>
    {/each}
    {#each mismatches as m (m.h.host)}
      <li>
        <span class="mono">{m.h.host}</span> goes through {laneName[m.h.lane]}, while the routes point at
        {laneName[m.want as LaneId]}. Expected for about a minute after a switch (DNS and the worker cache their answers).
      </li>
    {/each}
  </ul>
{/if}

<p class="subtle small legend">
  <span class="key observed"></span> observed: requested from this box through public DNS, as a visitor would
  · <span class="key inferred"></span> inferred from the routes
  · <span class="key idle"></span> not in use{Object.keys(targets).length ? ' (click a highlighted grey part to switch to it)' : ''}
  {#if blockedList.length}
    · <span class="key unavailable"></span> unavailable: {blockedList.map(([what, why]) => `${what.toLowerCase()} — ${why}`).join('; ')}
  {/if}
</p>

<style>
  .wrap {
    width: 100%;
    min-width: 0;
  }
  svg {
    display: block;
    max-width: 980px;
    margin: 0 auto;
    font-family: var(--font-sans);
  }
  .n {
    stroke-width: 1.5;
  }
  .n.observed {
    fill: var(--ok-bg);
    stroke: var(--ok-fg);
  }
  .n.inferred {
    fill: var(--info-bg);
    stroke: var(--info-fg);
    stroke-dasharray: 5 3;
  }
  .n.idle {
    fill: var(--surface);
    stroke: var(--border-strong);
  }
  .n.unavailable {
    fill: url(#rm-hatch);
    stroke: var(--border-strong);
    stroke-dasharray: 3 3;
  }
  .hatch-bg {
    fill: var(--surface-sunken);
  }
  .hatch-line {
    stroke: var(--border);
    stroke-width: 2;
  }
  .nl {
    font-size: 12px;
    font-weight: 600;
    fill: var(--text);
  }
  .nl.idle,
  .nl.unavailable {
    fill: var(--text-subtle);
  }
  .ns {
    font-size: 10px;
    fill: var(--text-subtle);
  }
  .e {
    fill: none;
  }
  .e.observed {
    stroke: var(--ok-fg);
    stroke-width: 2.5;
  }
  .e.inferred {
    stroke: var(--info-fg);
    stroke-width: 2;
    stroke-dasharray: 6 4;
  }
  .e.idle {
    stroke: var(--border-strong);
    stroke-width: 1.5;
  }
  .e.unavailable {
    stroke: var(--border-strong);
    stroke-width: 1.5;
    stroke-dasharray: 2 4;
    opacity: 0.7;
  }
  .arrowhead.observed {
    fill: var(--ok-fg);
  }
  .arrowhead.inferred {
    fill: var(--info-fg);
  }
  .arrowhead.idle,
  .arrowhead.unavailable {
    fill: var(--border-strong);
  }
  .el {
    font-size: 10.5px;
    fill: var(--text-muted);
    paint-order: stroke;
    stroke: var(--surface);
    stroke-width: 4px;
    stroke-linejoin: round;
  }
  .el.observed {
    fill: var(--ok-fg);
    font-weight: 600;
  }
  .el.idle,
  .el.unavailable {
    fill: var(--text-subtle);
  }
  .click {
    cursor: pointer;
    outline: none;
  }
  .ns.cta {
    fill: var(--primary);
  }
  .click:hover .n,
  .click:focus-visible .n,
  .n.sel {
    stroke: var(--primary);
    stroke-width: 2.5;
    stroke-dasharray: none;
  }
  .click:hover .e,
  .click:focus-visible .e,
  .e.sel {
    stroke: var(--primary);
    stroke-width: 2.5;
    stroke-dasharray: none;
  }
  .paths {
    list-style: none;
    padding: 0;
    margin: 0.6rem 0 0;
    display: grid;
    gap: 0.35rem;
  }
  .paths li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.3rem 0.5rem;
    min-width: 0;
  }
  .grow {
    flex: 1 1 12rem;
    min-width: 0;
  }
  .host {
    font-size: 0.78rem;
    overflow-wrap: anywhere;
  }
  .dot,
  .key {
    display: inline-block;
    width: 0.6rem;
    height: 0.6rem;
    border-radius: 50%;
    flex: none;
  }
  .key {
    width: 1.1rem;
    height: 0;
    border-radius: 0;
    vertical-align: middle;
  }
  .dot.observed {
    background: var(--ok-fg);
  }
  .dot.inferred {
    background: var(--info-fg);
  }
  .dot.neutral {
    background: var(--border-strong);
  }
  .key.observed {
    border-top: 2.5px solid var(--ok-fg);
  }
  .key.inferred {
    border-top: 2px dashed var(--info-fg);
  }
  .key.idle {
    border-top: 1.5px solid var(--border-strong);
  }
  .key.unavailable {
    border-top: 1.5px dotted var(--border-strong);
  }
  .mono {
    font-family: var(--font-mono);
  }
  .small {
    font-size: 0.8rem;
  }
  .confirm {
    margin-top: 0.75rem;
    padding: 0.7rem;
    border: 1px solid var(--primary);
    border-radius: 8px;
  }
  .confirm p {
    margin: 0 0 0.5rem;
  }
  .confirm label {
    display: flex;
    gap: 0.4rem;
    align-items: center;
    margin-bottom: 0.5rem;
  }
  .actions {
    display: flex;
    gap: 0.6rem;
  }
  .notes {
    margin: 0.6rem 0 0;
    padding-left: 1.1rem;
    color: var(--warn-fg);
  }
  .notes li + li {
    margin-top: 0.25rem;
  }
  .legend {
    margin: 0.6rem 0 0;
    line-height: 1.6;
  }
</style>
