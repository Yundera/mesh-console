<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import {
    api,
    type BoxCert,
    type Certificates,
    type CertRow,
    type CertStatus,
    type Holder,
    type NameKind,
    type Renewal,
    type ZoneState,
  } from '../lib/api'
  import { ago, dateTime } from '../lib/format'

  type Tone = 'ok' | 'warn' | 'bad' | 'info' | 'neutral'

  let data = $state<Certificates | null>(null)
  let error = $state('')
  let loading = $state(false)

  // On demand only: one TLS handshake per name the box certifies itself, two
  // from outside, and a read of Caddy's log.
  async function load() {
    loading = true
    try {
      data = await api.get<Certificates>('/api/certificates')
      error = ''
    } catch (e) {
      error = (e as Error).message
    } finally {
      loading = false
    }
  }

  $effect(() => {
    load()
  })

  const statusLabel: Record<CertStatus, string> = {
    letsencrypt: "Let's Encrypt",
    fallback: 'Fallback',
    unreachable: 'No certificate',
  }
  const statusTone = (s: CertStatus | undefined): Tone =>
    s === 'letsencrypt' ? 'ok' : s === 'fallback' ? 'warn' : s === 'unreachable' ? 'bad' : 'neutral'
  const renewalTone = (r: Renewal | undefined): Tone => (r === 'expired' ? 'bad' : r === 'overdue' ? 'warn' : 'neutral')

  function expiry(notAfter: string | undefined, renewal: Renewal | undefined): string {
    if (!notAfter) return ''
    if (renewal === 'expired') return `expired ${ago(notAfter)}`
    return `expires ${ago(notAfter)}`
  }

  const kindLabel: Record<NameKind, string> = {
    dash: 'dash name',
    dot: 'dot name',
    apex: 'your domain',
    sslip: 'sslip.io',
    ip: 'IP name',
    other: 'other',
  }

  // ---- your domain ---------------------------------------------------------
  const zoneLabel: Record<ZoneState, string> = {
    'not-delegated': 'Not delegated',
    pending: 'Switching',
    issuing: 'Waiting for certificate',
    ok: 'Own certificate',
    overdue: 'Renewal overdue',
    expired: 'Expired',
  }
  const zoneTone = (s: ZoneState): Tone =>
    s === 'ok' ? 'ok' : s === 'expired' ? 'bad' : s === 'not-delegated' ? 'neutral' : 'warn'

  const boxKindLabel: Record<BoxCert['kind'], string> = {
    letsencrypt: "Let's Encrypt",
    'mesh-ca': 'Mesh CA',
    other: 'Local certificate',
    unreachable: 'No certificate',
  }
  const boxTone = (b: BoxCert): Tone =>
    b.kind === 'letsencrypt' ? (b.renewal === 'ok' ? 'ok' : renewalTone(b.renewal)) : b.kind === 'unreachable' ? 'bad' : 'neutral'

  // ---- names and who decrypts them ----------------------------------------
  const domain = $derived(data?.domain ?? '')
  const dashExample = $derived(data?.domainName && data?.serverDomain ? `app-${data.domainName}.${data.serverDomain}` : 'app-<you>.<zone>')
  const ipZoneLabel = $derived(data?.ipZone ? `.${data.ipZone}` : 'IP-zone (IP_ZONE unset)')
  const dashOutside = $derived(data?.outside?.find((o) => o.kind === 'dash'))
  const dashHolder = $derived(
    dashOutside?.holder === 'gateway' ? 'the mesh gateway' : 'Cloudflare (the mesh gateway after the switch-over)',
  )
  const zoneLive = $derived(data?.zone?.state === 'ok' || data?.zone?.state === 'overdue')

  const holderLabel: Record<Holder, string> = {
    cloudflare: 'Cloudflare',
    gateway: 'Mesh gateway',
    box: 'Your box',
    unknown: 'Unknown',
  }

  // ---- addresses -----------------------------------------------------------
  const rows = $derived(data?.certs ?? [])
  const probed = $derived(rows.filter((r) => r.status))
  const counts = $derived({
    total: probed.length,
    letsencrypt: probed.filter((r) => r.status === 'letsencrypt').length,
    fallback: probed.filter((r) => r.status === 'fallback').length,
    unreachable: probed.filter((r) => r.status === 'unreachable').length,
  })
  // Per app (a name declared by two containers appears under both), problems first.
  const statusOrder = (r: CertRow) => (r.status === 'unreachable' ? 0 : r.status === 'fallback' ? 1 : 2)
  const kindOrder: Record<NameKind, number> = { dash: 0, dot: 1, apex: 2, sslip: 3, ip: 4, other: 5 }
  const apps = $derived.by(() => {
    const m = new Map<string, CertRow[]>()
    for (const r of rows) for (const s of r.sources) m.set(s, [...(m.get(s) ?? []), r])
    return [...m.entries()]
      .map(([name, list]) => ({
        name,
        rows: [...list].sort((a, b) => kindOrder[a.kind] - kindOrder[b.kind] || a.domain.localeCompare(b.domain)),
        worst: Math.min(...list.map(statusOrder)),
      }))
      .sort((a, b) => a.worst - b.worst || a.name.localeCompare(b.name))
  })

  function unprobedNote(k: NameKind): string {
    switch (k) {
      case 'dash':
        return `Certificate held by ${dashHolder}`
      case 'ip':
        return 'Box certificate (mesh CA), for the mesh only'
      case 'dot':
      case 'apex':
        return 'Usable in a browser once your domain is delegated'
      default:
        return 'Not checked'
    }
  }
</script>

<div class="stack">
  <Card title="Certificates">
    {#snippet actions()}
      <button onclick={load} disabled={loading}>{loading ? 'Checking…' : 'Check again'}</button>
    {/snippet}
    <p class="subtle small intro">
      Each name of your box has its certificate, and its private key, in exactly one place. Dash names
      (<span class="mono">{dashExample}</span>) are secured by the mesh in front of your box. Dot names and your
      domain itself are secured by your box alone, once your domain is delegated to the mesh nameservers. The
      <span class="mono">.sslip.io</span> addresses also carry your box's own certificate, so they work even when
      the mesh is down.
    </p>
    {#if error && !data}
      <p class="error">{error}</p>
    {:else if !data}
      <p class="subtle">Checking every address… this can take a few seconds.</p>
    {:else}
      <div class="kpis">
        <div class="kpi" class:ok={zoneLive} class:warn={data.zone && !zoneLive && data.zone.state !== 'not-delegated'}>
          <div class="n small-n">{data.zone ? zoneLabel[data.zone.state] : '—'}</div>
          <div class="subtle small">your domain</div>
        </div>
        <div class="kpi" class:ok={counts.total > 0 && counts.letsencrypt === counts.total}>
          <div class="n">{counts.letsencrypt}<span class="subtle of">/{counts.total}</span></div>
          <div class="subtle small">box certificates on Let's Encrypt</div>
        </div>
        <div class="kpi" class:warn={counts.fallback > 0}>
          <div class="n">{counts.fallback}</div>
          <div class="subtle small">fallback</div>
        </div>
        <div class="kpi" class:bad={counts.unreachable > 0}>
          <div class="n">{counts.unreachable}</div>
          <div class="subtle small">no certificate</div>
        </div>
      </div>
      <p class="subtle small">Checked {dateTime(data.snapshotAt)}</p>
      {#if error}<p class="error small">{error}</p>{/if}
      {#if data.backendError}
        <p class="small warn-text">
          The mesh server could not be reached ({data.backendError}), so the delegation state comes from this box alone.
        </p>
      {/if}
    {/if}
  </Card>

  {#if data}
    <Card title="Your domain">
      {#if !data.zone}
        <p class="subtle">The box domain is not known (DOMAIN unset and the mesh server unreachable).</p>
      {:else}
        {@const z = data.zone}
        <div class="head">
          <span class="mono domain">{z.domain}</span>
          <Pill tone={zoneTone(z.state)}>{zoneLabel[z.state]}</Pill>
        </div>
        {#if z.state === 'not-delegated'}
          <p class="small">
            Your domain is not delegated to the mesh nameservers yet. Your apps are reached on their dash names
            (<span class="mono">{dashExample}</span>), which work in any browser. Dot names
            (<span class="mono">app.{z.domain}</span>) carry no certificate a browser trusts until then.
          </p>
        {:else if z.state === 'pending'}
          <p class="small warn-text">
            The mesh nameservers serve your domain, but Caddy has not switched to obtaining its own certificate yet. It
            does at its next configuration refresh, within a few minutes.
          </p>
        {:else if z.state === 'issuing'}
          <p class="small warn-text">
            Your box is obtaining its own certificate from Let's Encrypt. Until it has one, dot names are not offered;
            dash names keep working.
          </p>
        {:else if z.state === 'ok'}
          <p class="subtle small">
            Your box holds its own Let's Encrypt certificate for your domain and every dot name. Caddy renews it on its
            own.
          </p>
        {:else if z.state === 'overdue'}
          <p class="small warn-text">Caddy should have renewed this certificate by now.</p>
        {:else}
          <p class="small error">This certificate has expired: browsers refuse your domain and its dot names.</p>
        {/if}
        {#if z.reason}
          <p class="small reason" class:error={z.state === 'expired'}>{z.reason}</p>
        {/if}
        {#if z.reasonDetail}
          <details>
            <summary class="subtle small">Caddy log line</summary>
            <pre>{z.reasonDetail}</pre>
          </details>
        {/if}

        <ul class="rows">
          {#each [{ label: 'Your domain', b: z.apex }, { label: 'Dot names', b: z.wildcard }] as item (item.label)}
            <li>
              <div class="head">
                <span class="label">{item.label}</span>
                <span class="mono subtle small">{item.b.domain}</span>
                <Pill tone={boxTone(item.b)}>{boxKindLabel[item.b.kind]}</Pill>
                {#if item.b.notAfter}
                  <Pill tone={renewalTone(item.b.renewal)}>{expiry(item.b.notAfter, item.b.renewal)}</Pill>
                {/if}
              </div>
              <div class="subtle small">
                {#if item.b.issuer}{item.b.issuer}{/if}
                {#if item.b.method}&nbsp;· obtained by DNS-01{/if}
                {#if item.b.kind === 'mesh-ca'}&nbsp;· trusted by the mesh only{/if}
              </div>
              {#if item.b.error}<p class="subtle small mono detail-inline">{item.b.error}</p>{/if}
            </li>
          {/each}
        </ul>
        {#if z.answer && z.state !== 'not-delegated'}
          <p class="subtle small">
            The mesh nameservers currently send visitors {z.answer === 'gateway' ? 'through the mesh gateway' : 'straight to your box'}.
            Either way, dot names stay encrypted up to your box.
          </p>
        {/if}
      {/if}
    </Card>

    <Card title="Where your traffic is decrypted">
      <table class="who">
        <thead>
          <tr><th>Name</th><th>Example</th><th>Decrypted by</th></tr>
        </thead>
        <tbody>
          <tr>
            <td>Dash names</td>
            <td class="mono">{dashExample}</td>
            <td>
              {dashHolder}, which then re-encrypts to your box.
              <span class="subtle">The mesh operator can read this traffic.</span>
            </td>
          </tr>
          <tr>
            <td>Dot names</td>
            <td class="mono">app.{domain || '<your domain>'}</td>
            <td>
              {#if zoneLive}
                Your box only. The mesh only passes the encrypted connection through.
              {:else}
                Your box only, once your domain is delegated and its certificate issued. Not available yet.
              {/if}
            </td>
          </tr>
          <tr>
            <td>Your domain</td>
            <td class="mono">{domain || '<your domain>'}</td>
            <td>
              {#if zoneLive}
                Your box. The mesh passes it through by policy: its own certificate for the whole mesh zone would also
                match this name.
              {:else}
                The mesh in front of your box, until your domain is delegated.
              {/if}
            </td>
          </tr>
          <tr>
            <td>sslip.io</td>
            <td class="mono">app-&lt;ip&gt;.sslip.io</td>
            <td>Your box only, with its own Let's Encrypt certificate.</td>
          </tr>
          <tr>
            <td>IP names</td>
            <td class="mono">app-&lt;ip&gt;{ipZoneLabel}</td>
            <td>Your box, with the mesh certificate. Used by the mesh to reach your box, not meant for browsers.</td>
          </tr>
        </tbody>
      </table>
      <p class="subtle small">
        This is about the network path: nothing between a visitor and your box can read a dot name's traffic. It does
        not cover the software that runs on your box, which you install and update from its publishers.
      </p>
    </Card>

    <Card title="Seen from the internet">
      {#if !data.outside || data.outside.length === 0}
        <p class="subtle">Nothing checked: the box's mesh name is not known.</p>
      {:else}
        <p class="subtle small intro">
          A connection made the way a visitor's browser makes it, from this box to its own public names. Your browser's
          certificate viewer shows the same.
        </p>
        <ul class="rows">
          {#each data.outside as o (o.host)}
            <li>
              <div class="head">
                <a class="mono domain" href={`https://${o.host}/`} target="_blank" rel="noopener">{o.host}</a>
                <Pill tone="neutral">{kindLabel[o.kind]}</Pill>
                {#if o.holder}
                  <Pill tone={o.verified ? 'ok' : 'bad'}>{holderLabel[o.holder]}</Pill>
                {:else}
                  <Pill tone="bad">No answer</Pill>
                {/if}
              </div>
              <div class="subtle small">
                {#if o.issuer}{o.issuer}{/if}
                {#if o.notAfter}&nbsp;· {expiry(o.notAfter, undefined)}{/if}
                {#if o.holder && !o.verified}&nbsp;· a browser would refuse this certificate{/if}
              </div>
              {#if o.error}<p class="subtle small mono detail-inline">{o.error}</p>{/if}
            </li>
          {/each}
        </ul>
        {#if !zoneLive && data.zone}
          <p class="subtle small">The dot name is checked once your box holds its own certificate.</p>
        {/if}
      {/if}
    </Card>

    <Card title="Mesh certificate (box identity)">
      {#if data.meshCert}
        {@const m = data.meshCert}
        <p class="subtle small intro">
          Issued to your box by the mesh. The mesh gateway checks it before sending traffic for your dash names, and it
          covers the IP names. Browsers do not trust it, and do not need to.
        </p>
        <dl class="kv">
          <dt>Box identity</dt>
          <dd>
            {#if m.hopOk}
              <span class="mono">{m.hopName}</span> <Pill tone="ok">present</Pill>
            {:else if m.hopName}
              <span class="mono">{m.hopName}</span> <Pill tone="bad">missing</Pill>
            {:else}
              <span class="subtle">unknown (the mesh server could not be reached)</span>
            {/if}
          </dd>
          <dt>IP names</dt>
          <dd class="mono">{m.ipNames.length ? m.ipNames.join(', ') : '—'}{#if m.ips.length}&nbsp;· {m.ips.join(', ')}{/if}</dd>
          {#if m.legacy.length || m.other.length}
            <dt>Older names</dt>
            <dd class="mono">{[...m.legacy, ...m.other].join(', ')}</dd>
          {/if}
          <dt>Issued by</dt>
          <dd>{m.issuer || '—'}</dd>
          <dt>Validity</dt>
          <dd>
            <Pill tone={m.renewal === 'ok' ? 'ok' : renewalTone(m.renewal)}>{expiry(m.notAfter, m.renewal)}</Pill>
            <span class="subtle small">{dateTime(m.notAfter)}</span>
          </dd>
        </dl>
        {#if m.hopName && !m.hopOk}
          <p class="small error">
            This certificate does not carry the box identity, so the mesh gateway refuses to forward traffic to your box
            until it renews. The mesh agent renews it on its own, at the latest a day and a half after the mesh server
            started adding it.
          </p>
        {:else if m.legacy.length}
          <p class="subtle small">It still carries names an older mesh server added; the next renewal drops them.</p>
        {/if}
        {#if m.renewal === 'overdue'}
          <p class="small warn-text">
            The mesh agent renews this certificate halfway through its life, so it should have been renewed by now. Check
            that the box reaches the mesh server (Diagnostics).
          </p>
        {:else if m.renewal === 'expired'}
          <p class="small error">
            This certificate has expired, so the mesh gateway cannot reach your box. Check that the box reaches the mesh
            server (Diagnostics).
          </p>
        {:else}
          <p class="subtle small">It lasts three days and the mesh agent renews it on its own.</p>
        {/if}
      {:else}
        <p class="error small">{data.meshCertError ?? 'Not found.'}</p>
      {/if}
    </Card>

    <Card title="Who may issue certificates for your domain (CAA)">
      {@const c = data.caa}
      {#if !c.delegated}
        <p class="subtle small">
          Once your domain is delegated, its DNS lists the Let's Encrypt accounts of this box as the only ones allowed to
          obtain a certificate for it. Not in effect yet: your domain is not delegated.
        </p>
      {:else}
        <p class="subtle small intro">
          Your domain's DNS allows only the Let's Encrypt accounts below to obtain a certificate for it. Your box reports
          them to the mesh, which publishes them. Any certificate issued is also recorded in public Certificate Transparency
          logs.
        </p>
        {#each c.issues ?? [] as issue}
          <p class="small warn-text">{issue}</p>
        {/each}
        {#if c.accounts.length === 0}
          <p class="subtle small">Caddy holds no Let's Encrypt account yet.</p>
        {:else}
          <ul class="rows">
            {#each c.accounts as a (a.uri)}
              <li>
                <div class="head">
                  <span class="mono small domain">{a.uri}</span>
                  {#if a.current}<Pill tone="info">in use</Pill>{/if}
                  {#if a.staging}<Pill tone="neutral">test</Pill>{/if}
                  <Pill tone={a.reported ? 'ok' : a.current ? 'warn' : 'neutral'}>{a.reported ? 'published' : 'not published'}</Pill>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
        <p class="subtle small">
          {#if c.reportedAt}Last reported {ago(c.reportedAt)} ({dateTime(c.reportedAt)}).{:else}Never reported.{/if}
        </p>
      {/if}
      {#if c.error}<p class="error small">{c.error}</p>{/if}
    </Card>

    <Card title="App addresses">
      {#if data.error}
        <p class="error">{data.error}</p>
      {:else if apps.length === 0}
        <p class="subtle">No running app declares an address.</p>
      {:else}
        {#if !data.sslipRoutes}
          <p class="subtle small">
            sslip.io addresses are turned off on this box (SSLIP_ROUTES=off): apps get none, only the box's root sslip.io
            address remains.
          </p>
        {/if}
        <ul class="rows">
          {#each apps as app (app.name)}
            <li>
              <div class="app-name">{app.name}</div>
              <ul class="names">
                {#each app.rows as r (r.domain)}
                  <li>
                    <div class="head">
                      <a class="mono domain" href={`https://${r.domain}/`} target="_blank" rel="noopener">{r.domain}</a>
                      <Pill tone="neutral">{kindLabel[r.kind]}</Pill>
                      {#if r.status}
                        <Pill tone={statusTone(r.status)}>{statusLabel[r.status]}</Pill>
                        {#if r.notAfter}
                          <Pill tone={renewalTone(r.renewal)}>{expiry(r.notAfter, r.renewal)}</Pill>
                        {/if}
                      {:else}
                        <span class="subtle small">{unprobedNote(r.kind)}</span>
                      {/if}
                    </div>
                    {#if r.issuer}<div class="subtle small">{r.issuer}</div>{/if}
                    {#if r.renewal === 'overdue' && r.status === 'letsencrypt'}
                      <p class="small warn-text">Caddy should have renewed this certificate by now.</p>
                    {/if}
                    {#if r.reason}
                      <p
                        class="small reason"
                        class:error={r.status === 'unreachable'}
                        class:warn-text={r.status === 'fallback'}>
                        {r.reason}
                      </p>
                    {/if}
                    {#if r.error}
                      <p class="subtle small mono detail-inline">{r.error}</p>
                    {/if}
                    {#if r.reasonDetail}
                      <details>
                        <summary class="subtle small">Caddy log line</summary>
                        <pre>{r.reasonDetail}</pre>
                      </details>
                    {/if}
                  </li>
                {/each}
              </ul>
            </li>
          {/each}
        </ul>
        {#if data.logError}<p class="subtle small">{data.logError}</p>{/if}
      {/if}
    </Card>
  {/if}
</div>

<style>
  .stack {
    display: grid;
    gap: 1rem;
  }
  .intro {
    margin-top: 0;
    max-width: 52rem;
  }
  .small {
    font-size: 0.8rem;
  }
  .kpis {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 0.75rem;
  }
  @media (max-width: 620px) {
    .kpis {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  .kpi {
    background: var(--surface-2);
    border-radius: 8px;
    padding: 0.7rem 0.8rem;
  }
  .kpi .n {
    font-size: 1.4rem;
    font-weight: 700;
  }
  .kpi .small-n {
    font-size: 1rem;
    line-height: 1.75rem;
  }
  .kpi .of {
    font-size: 0.9rem;
    font-weight: 500;
  }
  .kpi.ok .n {
    color: var(--ok-fg);
  }
  .kpi.warn .n {
    color: var(--warn-fg);
  }
  .kpi.bad .n {
    color: var(--bad-fg);
  }
  .warn-text {
    color: var(--warn-fg);
  }
  .rows,
  .names {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .rows > li {
    padding: 0.7rem 0;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }
  .rows > li:first-child {
    padding-top: 0;
  }
  .rows > li:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }
  .names > li {
    padding: 0.35rem 0 0.35rem 0.8rem;
    border-left: 2px solid var(--border);
    min-width: 0;
  }
  .app-name {
    font-weight: 600;
    margin-bottom: 0.3rem;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.4rem 0.6rem;
    margin-bottom: 0.2rem;
  }
  .label {
    font-weight: 600;
  }
  .domain {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .reason {
    margin: 0.35rem 0 0;
  }
  .detail-inline {
    margin: 0.25rem 0 0;
    overflow-wrap: anywhere;
  }
  details {
    margin-top: 0.3rem;
  }
  summary {
    cursor: pointer;
  }
  pre {
    margin: 0.4rem 0 0;
    padding: 0.5rem 0.7rem;
    background: var(--surface-2);
    border-radius: 6px;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 10rem;
    overflow: auto;
  }
  .who {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85rem;
  }
  .who th,
  .who td {
    text-align: left;
    vertical-align: top;
    padding: 0.45rem 0.6rem 0.45rem 0;
    border-bottom: 1px solid var(--border);
  }
  .who th {
    font-weight: 600;
    color: var(--text-muted);
  }
  .who td.mono {
    overflow-wrap: anywhere;
    font-size: 0.8rem;
  }
  @media (max-width: 620px) {
    .who thead {
      display: none;
    }
    .who tr {
      display: block;
      padding: 0.45rem 0;
      border-bottom: 1px solid var(--border);
    }
    .who td {
      display: block;
      border: none;
      padding: 0.1rem 0;
    }
    .who td:first-child {
      font-weight: 600;
    }
  }
</style>
