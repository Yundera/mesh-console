<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import { api, ApiError, type DomainInfo } from '../lib/api'

  let info = $state<DomainInfo | null>(null)
  let error = $state('')
  let host = $state('')
  let port = $state<number | ''>('')
  let saving = $state(false)
  let result = $state<{ ok: boolean; message: string; output?: string } | null>(null)

  async function load() {
    try {
      info = await api.get<DomainInfo>('/api/domain')
      error = ''
      if (!host) {
        host = info.defaultHost
        port = Number(info.defaultPort) || ''
      }
    } catch (e) {
      error = (e as Error).message
    }
  }
  $effect(() => {
    load()
  })

  const selected = $derived(info?.candidates?.find((c) => c.name === host))
  const ports = $derived(selected?.ports ?? [])
  const changed = $derived(!!info && (host !== info.defaultHost || String(port) !== info.defaultPort))

  function pickHost(name: string) {
    host = name
    const c = info?.candidates?.find((x) => x.name === name)
    if (c?.ports?.length && !c.ports.includes(Number(port))) port = c.ports[0]
  }

  async function save() {
    if (port === '') return
    result = null
    saving = true
    try {
      const res = await api.post<{ result: { exitCode: number; output: string }; rootDomain?: { status: number; error?: string } }>(
        '/api/domain/default-app',
        { host, port: Number(port) },
      )
      const rd = res.rootDomain
      result = {
        ok: !rd || (!rd.error && rd.status < 500),
        message: rd
          ? rd.error
            ? `Saved, but the root domain does not answer yet: ${rd.error}`
            : rd.status >= 500
              ? `Saved, but the root domain answers HTTP ${rd.status}. Is ${host} listening on port ${port}?`
              : `Saved. The root domain answers HTTP ${rd.status}.`
          : 'Saved.',
      }
      await load()
    } catch (e) {
      const body = e instanceof ApiError ? (e.body as { result?: { output?: string } } | undefined) : undefined
      result = {
        ok: false,
        message: e instanceof ApiError && e.status === 409 ? 'Another host action is running — try again in a moment.' : (e as Error).message,
        output: body?.result?.output,
      }
    } finally {
      saving = false
    }
  }
</script>

<div class="stack">
  <Card title="Your domain">
    {#if error}
      <p class="error">{error}</p>
    {:else if !info}
      <p class="subtle">Loading…</p>
    {:else}
      <dl class="kv">
        <dt>Domain</dt>
        <dd><a href={`https://${info.domain}`} target="_blank" rel="noopener">{info.domain}</a></dd>
        <dt>Apps</dt>
        <dd class="mono">&lt;app&gt;-{info.domain}</dd>
        {#if info.ipDash}
          <dt>Direct (Let’s Encrypt)</dt>
          <dd class="mono">{info.ipDash}.sslip.io</dd>
          <dt>Direct (mesh CA)</dt>
          <dd class="mono">{info.ipDash}.nip.io</dd>
        {/if}
        <dt>Root domain serves</dt>
        <dd class="mono">{info.defaultHost}:{info.defaultPort}</dd>
      </dl>
    {/if}
  </Card>

  <Card title="Default application">
    <p class="muted small">
      The app answering on <strong>{info?.domain ?? 'your domain'}</strong> itself and on any custom domain pointed at
      this box. Saving rewrites the mesh <code>.env</code> and recreates the mesh stack; public URLs blink for a few
      seconds.
    </p>
    {#if info}
      {#if info.candidatesError}<p class="error">{info.candidatesError}</p>{/if}
      <div class="form">
        <label>
          <span>Container</span>
          <select value={host} onchange={(e) => pickHost((e.currentTarget as HTMLSelectElement).value)}>
            {#if !info.candidates?.some((c) => c.name === host)}
              <option value={host}>{host}</option>
            {/if}
            {#each info.candidates ?? [] as c (c.name)}
              <option value={c.name} disabled={!c.onPcs}>
                {c.name}{c.project ? ` (${c.project})` : ''}{c.onPcs ? '' : ` — not on ${info.network}`}{c.state !== 'running' ? ` — ${c.state}` : ''}
              </option>
            {/each}
          </select>
        </label>
        <label>
          <span>Port</span>
          {#if ports.length}
            <select bind:value={port}>
              {#each ports as p (p)}<option value={p}>{p}</option>{/each}
            </select>
          {:else}
            <input type="number" min="1" max="65535" bind:value={port} />
          {/if}
        </label>
        <button class="primary" onclick={save} disabled={!changed || saving || port === ''}>
          {saving ? 'Applying…' : 'Save & apply'}
        </button>
      </div>
      {#if selected && !selected.onPcs}
        <p class="error small">Caddy reaches apps over the {info.network} network; this container is not on it.</p>
      {/if}
      {#if result}
        <p>
          <Pill tone={result.ok ? 'ok' : 'bad'}>{result.ok ? 'done' : 'problem'}</Pill>
          {result.message}
        </p>
        {#if result.output}<pre class="out">{result.output}</pre>{/if}
      {/if}
    {/if}
  </Card>

  <Card title="Use a custom domain">
    {#if info}
      <p class="muted small">
        Any domain you own can point at this box; until you choose otherwise it is served by the default application
        above.
      </p>
      <h3>Behind Cloudflare (proxied)</h3>
      <p class="small">
        Add a <strong>CNAME</strong> record for your hostname with target
        <code>{info.ipDash ? `${info.ipDash}.sslip.io` : '<your-ip>.sslip.io'}</code>, proxied, SSL mode <em>Full</em>.
      </p>
      <h3>Direct DNS</h3>
      <p class="small">
        Add an <strong>A</strong> record (or <strong>AAAA</strong> for IPv6) pointing at
        <code>{info.publicIp || 'your public IP'}</code>. The box must be reachable on ports 80 and 443.
      </p>
    {/if}
  </Card>
</div>

<style>
  .stack {
    display: grid;
    gap: 1rem;
  }
  .small {
    font-size: 0.8rem;
  }
  .form {
    display: flex;
    gap: 0.75rem;
    align-items: flex-end;
    flex-wrap: wrap;
    margin: 0.75rem 0;
  }
  label {
    display: grid;
    gap: 0.25rem;
  }
  label span {
    font-size: 0.75rem;
    color: var(--text-muted);
  }
  select {
    min-width: 14rem;
    max-width: 100%;
  }
  input[type='number'] {
    width: 7rem;
  }
  h3 {
    font-size: 0.85rem;
    margin: 0.9rem 0 0.3rem;
  }
  .out {
    background: var(--surface-sunken);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.6rem;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    max-height: 240px;
    overflow: auto;
    white-space: pre-wrap;
  }
</style>
