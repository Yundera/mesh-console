<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import EncryptionPaths from '../lib/EncryptionPaths.svelte'
  import { buildPaths } from '../lib/encryption'
  import {
    api,
    type BoxCert,
    type Certificates,
    type CertRow,
    type CertStatus,
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

  // ---- encryption paths ---------------------------------------------------
  const paths = $derived(data ? buildPaths(data) : [])
  const zoneLive = $derived(data?.zone?.state === 'ok' || data?.zone?.state === 'overdue')
  const dashExample = $derived(data?.domainName && data?.serverDomain ? `app-${data.domainName}.${data.serverDomain}` : 'app-<you>.<zone>')
  const dashHolder = $derived(
    data?.outside?.find((o) => o.kind === 'dash')?.holder === 'gateway' ? 'the mesh gateway' : 'Cloudflare',
  )

  // ---- addresses -----------------------------------------------------------
  // IP names are the mesh's inside leg, not addresses to open: left to Mesh internals.
  const rows = $derived((data?.certs ?? []).filter((r) => r.kind !== 'ip'))
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
      case 'dot':
      case 'apex':
        return 'Usable in a browser once your domain is delegated'
      default:
        return 'Not checked'
    }
  }
  // One chip per address: the certificate's state, or what is known without a check.
  const chipTone = (r: CertRow): Tone =>
    r.status ? (r.status === 'letsencrypt' ? renewalTone(r.renewal) === 'neutral' ? 'ok' : renewalTone(r.renewal) : statusTone(r.status)) : 'neutral'
  const problem = (r: CertRow) => !!(r.reason || r.error || (r.status && r.status !== 'letsencrypt') || r.renewal === 'overdue' || r.renewal === 'expired')
  const meshInternalsOpen = $derived(
    !!data && (!data.meshCert || (!!data.meshCert.hopName && !data.meshCert.hopOk) || data.meshCert.renewal !== 'ok'),
  )
</script>

<div class="stack">
  <Card title="How your addresses are encrypted">
    {#snippet actions()}
      <button onclick={load} disabled={loading}>{loading ? 'Checking…' : 'Check again'}</button>
    {/snippet}
    <p class="subtle small intro">
      Every address below opens in a browser without a warning. Each line is the encrypted connection from the browser to
      your box: where it is decrypted, which authority signed the certificate there, and who holds that certificate's key.
      <b>End to end</b>: only your box can read the traffic. <b>Decrypted by …</b>: the mesh reads it, then re-encrypts it
      to your box.
    </p>
    {#if error && !data}
      <p class="error">{error}</p>
    {:else if !data}
      <p class="subtle">Checking every address… this can take a few seconds.</p>
    {:else}
      <EncryptionPaths {paths} />
      <p class="subtle small">
        Checked {dateTime(data.snapshotAt)}. This is about the network path, not the software on your box, which you install
        and update from its publishers.
      </p>
      {#if error}<p class="error small">{error}</p>{/if}
      {#if data.backendError}
        <p class="small warn-text">
          The mesh server could not be reached ({data.backendError}), so the delegation state and the route to your box come
          from this box alone.
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
            (<span class="mono">{dashExample}</span>), which the mesh decrypts. Dot names
            (<span class="mono">app.{z.domain}</span>), encrypted end to end, need the delegation.
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
      {/if}

      {@const c = data.caa}
      <h3>Who may issue certificates for it (CAA)</h3>
      {#if !c.delegated}
        <p class="subtle small">
          Once your domain is delegated, its DNS lists the Let's Encrypt accounts of this box as the only ones allowed to
          obtain a certificate for it. Not in effect yet.
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
        <p class="subtle small intro">
          {#if counts.total}
            Your box obtains {counts.total} certificate{counts.total === 1 ? '' : 's'} itself:
            {counts.letsencrypt} on Let's Encrypt{#if counts.fallback}, {counts.fallback} on a fallback{/if}{#if counts.unreachable}, {counts.unreachable} with none{/if}.
          {/if}
          Dash names carry {dashHolder}'s certificate{zoneLive ? '' : '; dot names wait for the delegation'}.
          {#if !data.sslipRoutes}Apps get no sslip.io address on this box (SSLIP_ROUTES=off).{/if}
        </p>
        <ul class="rows">
          {#each apps as app (app.name)}
            <li>
              <div class="head">
                <span class="app-name">{app.name}</span>
                {#each app.rows as r (r.domain)}
                  <a
                    class="chip {chipTone(r)}"
                    href={`https://${r.domain}/`}
                    target="_blank"
                    rel="noopener"
                    title={`${r.domain}${r.status ? '' : ' · ' + unprobedNote(r.kind)}`}>
                    <span class="dot"></span>{kindLabel[r.kind]}
                  </a>
                {/each}
              </div>
              {#each app.rows.filter(problem) as r (r.domain)}
                <div class="problem">
                  <span class="mono small domain">{r.domain}</span>
                  {#if r.status}<Pill tone={statusTone(r.status)}>{statusLabel[r.status]}</Pill>{/if}
                  {#if r.notAfter && r.renewal !== 'ok'}
                    <Pill tone={renewalTone(r.renewal)}>{expiry(r.notAfter, r.renewal)}</Pill>
                  {/if}
                  {#if r.reason}
                    <p class="small reason" class:error={r.status === 'unreachable'} class:warn-text={r.status === 'fallback'}>
                      {r.reason}
                    </p>
                  {/if}
                  {#if r.error}<p class="subtle small mono detail-inline">{r.error}</p>{/if}
                  {#if r.reasonDetail}
                    <details>
                      <summary class="subtle small">Caddy log line</summary>
                      <pre>{r.reasonDetail}</pre>
                    </details>
                  {/if}
                </div>
              {/each}
              <details>
                <summary class="subtle small">Addresses</summary>
                <ul class="names">
                  {#each app.rows as r (r.domain)}
                    <li>
                      <a class="mono small domain" href={`https://${r.domain}/`} target="_blank" rel="noopener">{r.domain}</a>
                      <span class="subtle small">
                        · {kindLabel[r.kind]}
                        {#if r.status}· {r.issuer || statusLabel[r.status]}{#if r.notAfter} · {expiry(r.notAfter, r.renewal)}{/if}
                        {:else}· {unprobedNote(r.kind)}{/if}
                      </span>
                    </li>
                  {/each}
                </ul>
              </details>
            </li>
          {/each}
        </ul>
        {#if data.logError}<p class="subtle small">{data.logError}</p>{/if}
      {/if}
    </Card>

    <Card title="Mesh internals">
      <details open={meshInternalsOpen}>
        <summary class="small">
          The certificate the mesh uses to reach your box (cert0)
          {#if data.meshCert}
            <Pill tone={data.meshCert.hopName && !data.meshCert.hopOk ? 'bad' : data.meshCert.renewal === 'ok' ? 'ok' : renewalTone(data.meshCert.renewal)}>
              {data.meshCert.hopName && !data.meshCert.hopOk ? 'box name missing' : expiry(data.meshCert.notAfter, data.meshCert.renewal)}
            </Pill>
          {:else}
            <Pill tone="bad">not found</Pill>
          {/if}
        </summary>
        {#if data.meshCert}
          {@const m = data.meshCert}
          <p class="subtle small intro">
            Signed by the Mesh Router CA, a private authority only the mesh's machines trust. It ends the inside leg of a
            dash name: the mesh gateway connects to your box's name below, Cloudflare's Worker to its IP names. Browsers
            never see it.
          </p>
          <dl class="kv">
            <dt>Box name</dt>
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
              This certificate does not carry the box name, so the mesh gateway refuses to forward traffic to your box until
              it renews. The mesh agent renews it on its own, at the latest a day and a half after the mesh server started
              adding it.
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
              This certificate has expired, so the mesh cannot reach your box. Check that the box reaches the mesh server
              (Diagnostics).
            </p>
          {:else}
            <p class="subtle small">It lasts three days and the mesh agent renews it on its own.</p>
          {/if}
        {:else}
          <p class="error small">{data.meshCertError ?? 'Not found.'}</p>
        {/if}
      </details>
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
  h3 {
    font-size: 0.9rem;
    margin: 1.2rem 0 0.4rem;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.1rem 0.55rem;
    border-radius: var(--radius-pill);
    font-size: 0.75rem;
    font-weight: 600;
    text-decoration: none;
    background: var(--surface-3);
    color: var(--text-muted);
  }
  .chip .dot {
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 50%;
    background: currentColor;
  }
  .chip.ok {
    background: var(--ok-bg);
    color: var(--ok-fg);
  }
  .chip.warn {
    background: var(--warn-bg);
    color: var(--warn-fg);
  }
  .chip.bad {
    background: var(--bad-bg);
    color: var(--bad-fg);
  }
  .problem {
    margin: 0.4rem 0 0;
    padding-left: 0.8rem;
    border-left: 2px solid var(--warn-fg);
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
</style>
