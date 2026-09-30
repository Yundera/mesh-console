<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import { api, type Certificates, type CertRow, type CertStatus, type Renewal } from '../lib/api'
  import { ago, dateTime } from '../lib/format'

  let data = $state<Certificates | null>(null)
  let error = $state('')
  let loading = $state(false)

  // On demand only: one TLS handshake per address plus a read of Caddy's log.
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
  const statusTone = (s: CertStatus) => (s === 'letsencrypt' ? 'ok' : s === 'fallback' ? 'warn' : 'bad')
  const renewalTone = (r: Renewal | undefined) => (r === 'expired' ? 'bad' : r === 'overdue' ? 'warn' : 'neutral')

  function expiry(notAfter: string | undefined, renewal: Renewal | undefined): string {
    if (!notAfter) return ''
    if (renewal === 'expired') return `expired ${ago(notAfter)}`
    return `expires ${ago(notAfter)}`
  }

  const rows = $derived(data?.certs ?? [])
  const counts = $derived({
    total: rows.length,
    letsencrypt: rows.filter((r) => r.status === 'letsencrypt').length,
    fallback: rows.filter((r) => r.status === 'fallback').length,
    unreachable: rows.filter((r) => r.status === 'unreachable').length,
  })
  // Problems first, then alphabetical (the server sorts by domain).
  const order: Record<CertStatus, number> = { unreachable: 0, fallback: 1, letsencrypt: 2 }
  const sorted = $derived([...rows].sort((a: CertRow, b: CertRow) => order[a.status] - order[b.status]))
</script>

<div class="stack">
  <Card title="Certificates">
    {#snippet actions()}
      <button onclick={load} disabled={loading}>{loading ? 'Checking…' : 'Check again'}</button>
    {/snippet}
    <p class="subtle small intro">
      Your box answers on three kinds of address. Your domain and the <span class="mono">.nip.io</span> addresses
      use the mesh certificate, which the mesh server issues and trusts. The <span class="mono">.sslip.io</span>
      addresses get a certificate from Let's Encrypt, so they work in any browser even when the mesh is down. If
      Let's Encrypt refuses, Caddy serves a fallback certificate instead, and browsers show a warning on that
      address.
    </p>
    {#if error && !data}
      <p class="error">{error}</p>
    {:else if !data}
      <p class="subtle">Checking every address… this can take a few seconds.</p>
    {:else}
      <div class="kpis">
        <div class="kpi">
          <div class="n">{counts.total}</div>
          <div class="subtle small">sslip.io addresses</div>
        </div>
        <div class="kpi" class:ok={counts.total > 0 && counts.letsencrypt === counts.total}>
          <div class="n">{counts.letsencrypt}</div>
          <div class="subtle small">Let's Encrypt</div>
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
    {/if}
  </Card>

  {#if data}
    <Card title="Mesh certificate">
      {#if data.gateway}
        {@const g = data.gateway}
        <dl class="kv">
          <dt>Covers</dt>
          <dd>Your domain and the <span class="mono">.nip.io</span> addresses <span class="subtle small">({g.dnsNames?.length ?? 0} names)</span></dd>
          <dt>Issued by</dt>
          <dd>{g.issuer || '—'}</dd>
          <dt>Validity</dt>
          <dd>
            <Pill tone={g.renewal === 'ok' ? 'ok' : renewalTone(g.renewal)}>{expiry(g.notAfter, g.renewal)}</Pill>
            <span class="subtle small">{dateTime(g.notAfter)}</span>
          </dd>
        </dl>
        {#if g.renewal === 'overdue'}
          <p class="small warn-text">This certificate should have been renewed by now. Run an update to renew it.</p>
        {:else if g.renewal === 'expired'}
          <p class="small error">This certificate has expired, so your domain does not work. Run an update to renew it.</p>
        {:else}
          <p class="subtle small">It lasts a few days and renews on its own well before it expires.</p>
        {/if}
      {:else}
        <p class="error small">{data.gatewayError ?? 'Not found.'}</p>
      {/if}
    </Card>

    <Card title="sslip.io addresses">
      {#if data.error}
        <p class="error">{data.error}</p>
      {:else if sorted.length === 0}
        <p class="subtle">No running app declares an sslip.io address.</p>
      {:else}
        <ul class="rows">
          {#each sorted as r (r.domain)}
            <li>
              <div class="head">
                <a class="mono domain" href={`https://${r.domain}/`} target="_blank" rel="noopener">{r.domain}</a>
                <Pill tone={statusTone(r.status)}>{statusLabel[r.status]}</Pill>
                {#if r.notAfter}
                  <Pill tone={renewalTone(r.renewal)}>{expiry(r.notAfter, r.renewal)}</Pill>
                {/if}
              </div>
              <div class="subtle small">
                {r.sources.join(', ')}{#if r.issuer}&nbsp;· {r.issuer}{/if}
              </div>
              {#if r.renewal === 'overdue' && r.status === 'letsencrypt'}
                <p class="small warn-text">Caddy should have renewed this certificate by now.</p>
              {/if}
              {#if r.reason}
                <p class="small reason" class:error={r.status === 'unreachable'} class:warn-text={r.status === 'fallback'}>
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
  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .rows li {
    padding: 0.7rem 0;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }
  .rows li:first-child {
    padding-top: 0;
  }
  .rows li:last-child {
    border-bottom: none;
    padding-bottom: 0;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.4rem 0.6rem;
    margin-bottom: 0.2rem;
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
