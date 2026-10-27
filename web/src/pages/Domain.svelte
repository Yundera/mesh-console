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

  let formSaving = $state<'dash' | 'dot' | null>(null)
  let formResult = $state<{ ok: boolean; message: string; output?: string } | null>(null)
  const pf = $derived(info?.primaryForm)

  async function setForm(form: 'dash' | 'dot') {
    formResult = null
    formSaving = form
    try {
      await api.post('/api/domain/primary-form', { form })
      formResult = {
        ok: true,
        message:
          form === 'dot'
            ? 'Done. Maison restarts each app on its new address: a minute or so of errors per app, and one sign-in per app.'
            : 'Done. Apps are back on their dash addresses.',
      }
      await load()
    } catch (e) {
      const body = e instanceof ApiError ? (e.body as { result?: { output?: string } } | undefined) : undefined
      formResult = { ok: false, message: (e as Error).message, output: body?.result?.output }
    } finally {
      formSaving = null
    }
  }

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
        // 409 is "busy" — another host action, or a self-check holding the lock;
        // the server says which.
        message: (e as Error).message,
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
        <dd class="mono">
          {#if info.primaryForm?.active === 'dot'}&lt;app&gt;.{info.domain}{:else}&lt;app&gt;-{info.domain}{/if}
        </dd>
        {#if info.ipDash}
          <dt>Direct (Let’s Encrypt)</dt>
          <dd class="mono">{info.ipDash}.sslip.io</dd>
          <dt>Direct (mesh CA)</dt>
          <dd class="mono">
            {#if info.ipZone}{info.ipDash}.{info.ipZone}{:else}<Pill tone="bad">IP_ZONE unset</Pill>
              <span class="subtle small">no direct route: run the self-check, which takes it from the mesh server</span>{/if}
          </dd>
        {/if}
        <dt>Root domain serves</dt>
        <dd class="mono">{info.defaultHost}:{info.defaultPort}</dd>
      </dl>
    {/if}
  </Card>

  {#if pf}
    <Card title="App addresses">
      <p class="muted small">
        Every app answers on both <code>&lt;app&gt;-{info?.domain}</code> and <code>&lt;app&gt;.{info?.domain}</code>.
        This picks the one Maison opens and gives the apps as their own address. The dot form needs this box's
        domain to be served by the mesh nameservers, with its certificate issued. Platform pages (this console,
        Maison's sign-in) keep their dash addresses.
      </p>
      <div class="choices">
        <button class:primary={pf.active === 'dash'} onclick={() => setForm('dash')}
          disabled={!pf.editable || formSaving !== null || pf.setting === 'dash'}>
          {formSaving === 'dash' ? 'Applying…' : `app-${info?.domain}`}
        </button>
        <button class:primary={pf.active === 'dot'} onclick={() => setForm('dot')}
          disabled={!pf.editable || formSaving !== null || pf.setting === 'dot' || !pf.dotReady}>
          {formSaving === 'dot' ? 'Applying…' : `app.${info?.domain}`}
        </button>
      </div>
      {#if !pf.editable}
        <p class="small">Changing it from here is {pf.blocked ?? 'disabled on this box'}.</p>
      {:else if pf.setting === 'dot' && pf.active !== 'dot'}
        <p class="small warn-text">
          The dot form is chosen but not in effect: this box cannot serve it right now (its domain is not served by the
          mesh nameservers, or the certificate is missing). Apps stay on their dash addresses until it can.
        </p>
      {:else if !pf.dotReady}
        <p class="subtle small">
          The dot form becomes available once this box's domain is served by the mesh nameservers and its certificate
          is issued.
        </p>
      {/if}
      {#if formResult}
        <p>
          <Pill tone={formResult.ok ? 'ok' : 'bad'}>{formResult.ok ? 'done' : 'problem'}</Pill>
          {formResult.message}
        </p>
        {#if formResult.output}<pre class="out">{formResult.output}</pre>{/if}
      {/if}
    </Card>
  {/if}

  <Card title="Default application">
    <p class="muted small">
      The app answering on <strong>{info?.domain ?? 'your domain'}</strong> itself and on any custom domain pointed at
      this box. Saving hands it to the box's template, which stores it and recreates the router and the login
      registrar; public URLs blink for a few seconds.
    </p>
    {#if info && !info.editable}
      <p class="small">
        Currently <code>{info.defaultHost}:{info.defaultPort}</code>. Changing it from here is {info.editBlocked ??
          'disabled on this box'}.
      </p>
    {:else if info}
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
  .warn-text {
    color: var(--warn-fg);
  }
  .choices {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin: 0.75rem 0;
  }
  .choices button {
    font-family: var(--font-mono, monospace);
  }
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
