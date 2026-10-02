<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import {
    api,
    ApiError,
    type Migration,
    type MigrationStatus,
    type MigrationStep,
    type Preflight,
  } from '../lib/api'
  import { ago, dateTime } from '../lib/format'

  let m = $state<Migration | null>(null)
  let loadError = $state('')
  let target = $state('')
  let statusUrl = $state('')
  let showAdvanced = $state(false)
  let preflight = $state<Preflight | null>(null)
  let busy = $state<'' | 'key' | 'preflight' | 'start' | 'cancel'>('')
  let actionError = $state('')
  let showLog = $state(false)
  let copied = $state(false)

  async function load() {
    try {
      m = await api.get<Migration>('/api/migration')
      loadError = ''
    } catch (e) {
      loadError = (e as Error).message
    }
  }

  const st = $derived<MigrationStatus | undefined>(m?.status)
  const running = $derived(!!st && ['starting', 'running', 'rolling_back'].includes(st.phase))
  const retired = $derived(m?.hold?.startsWith('retired:') ? m.hold.slice('retired:'.length) : '')
  // The preflight verdict only counts for the target it was run against.
  const cleared = $derived(!!preflight?.ok && preflight.target === target.trim())

  $effect(() => {
    load()
  })

  $effect(() => {
    const t = setInterval(load, running ? 3000 : 30_000)
    return () => clearInterval(t)
  })

  async function act<T>(kind: typeof busy, fn: () => Promise<T>): Promise<T | undefined> {
    actionError = ''
    busy = kind
    try {
      return await fn()
    } catch (e) {
      actionError = e instanceof ApiError && e.status === 409 ? e.message : (e as Error).message
      const out = (e as ApiError).body as { output?: string } | undefined
      if (out?.output) actionError += '\n' + out.output.trim().split('\n').slice(-6).join('\n')
    } finally {
      busy = ''
    }
  }

  const createKey = () => act('key', async () => {
    await api.post('/api/migration/key')
    await load()
  })

  const check = () => act('preflight', async () => {
    preflight = null
    const r = await api.post<{ preflight: Preflight }>('/api/migration/preflight', { target: target.trim() })
    preflight = r.preflight
  })

  const start = () => {
    if (!confirm(
      `Move this box to ${target.trim()}?\n\nYour apps stop during the final copy and come back on the new machine. ` +
      `This box is then retired: it stays up, but no longer serves your domain.`,
    )) return
    act('start', async () => {
      await api.post('/api/migration/start', { target: target.trim(), statusUrl: statusUrl.trim() })
      preflight = null
      setTimeout(load, 1500)
    })
  }

  const cancel = () => {
    if (!confirm('Cancel the migration? It stops at the next safe point and this box serves again.')) return
    act('cancel', async () => {
      await api.post('/api/migration/cancel')
      await load()
    })
  }

  async function copyKey() {
    if (!m?.key) return
    try {
      await navigator.clipboard.writeText(m.key)
      copied = true
      setTimeout(() => (copied = false), 2000)
    } catch {
      // Clipboard needs a secure context; the key stays selectable.
    }
  }

  const phaseTone = (p: MigrationStatus['phase']) =>
    p === 'done' ? 'ok' : p === 'failed' ? 'bad' : p === 'rolled_back' || p === 'cancelled' ? 'warn' : 'info'
  const phaseLabel: Record<MigrationStatus['phase'], string> = {
    starting: 'starting',
    running: 'running',
    rolling_back: 'rolling back',
    done: 'done',
    failed: 'failed',
    rolled_back: 'rolled back',
    cancelled: 'cancelled',
  }
  const stepMark = (s: MigrationStep['status']) =>
    s === 'success' ? '✓' : s === 'failed' ? '✗' : s === 'running' ? '…' : s === 'skipped' ? '–' : '·'
  const stepTone = (s: MigrationStep['status']) =>
    s === 'success' ? 'ok' : s === 'failed' ? 'bad' : s === 'running' ? 'info' : 'neutral'
  const groupLabel: Record<MigrationStep['group'], string> = {
    source: 'Apps online on this box',
    down: 'Apps stopped',
    target: 'Apps online on the new box',
  }
  const checkTone = (s: string) => (s === 'ok' ? 'ok' : s === 'warn' ? 'warn' : 'bad')
  const firstOfGroup = (steps: MigrationStep[], i: number) => i === 0 || steps[i - 1].group !== steps[i].group
</script>

<div class="stack">
  {#if loadError}
    <p class="error">{loadError}</p>
  {:else if !m}
    <p class="subtle">Loading…</p>
  {:else}
    {#if retired}
      <Card title="This box is retired">
        <p>
          It was migrated to <span class="mono">{retired}</span>, which now serves your domain. It stays up and reachable
          on its own address, but it no longer publishes routes and its self-check keeps it that way.
        </p>
        <p class="muted small">
          Delete the machine once you are happy with the new one. To bring this box back instead, remove
          <span class="mono">MESH_ROUTING_HOLD</span> from its <span class="mono">.env</span> and run the self-check, after
          taking the new box off the domain.
        </p>
      </Card>
    {/if}

    {#if m.arrived && m.arrived.phase === 'done'}
      <Card title="Migrated here">
        <p>
          This box was moved here from <span class="mono">{m.arrived.source.ip ?? 'another machine'}</span>
          {#if m.arrived.finishedAt}on {dateTime(m.arrived.finishedAt)}{/if}.
        </p>
      </Card>
    {/if}

    {#if st}
      <Card title="Migration">
        {#snippet actions()}
          {#if running}
            <button onclick={cancel} disabled={busy !== '' || st.phase === 'rolling_back'}>
              {busy === 'cancel' ? 'Cancelling…' : 'Cancel'}
            </button>
          {/if}
        {/snippet}
        <div class="run-head">
          <Pill tone={phaseTone(st.phase)}>{phaseLabel[st.phase] ?? st.phase}</Pill>
          <span>to <span class="mono">{st.target.user}@{st.target.host}</span></span>
          {#if st.startedAt}<span class="subtle">· started {dateTime(st.startedAt)}</span>{/if}
          {#if st.finishedAt}<span class="subtle">· finished {ago(st.finishedAt)}</span>{/if}
        </div>
        {#if st.error}<p class="error">{st.error}</p>{/if}
        <ol class="steps">
          {#each st.steps as s, i (s.key)}
            {#if firstOfGroup(st.steps, i)}
              <li class="group">{groupLabel[s.group] ?? s.group}</li>
            {/if}
            <li>
              <Pill tone={stepTone(s.status)}>{stepMark(s.status)}</Pill>
              <span class:subtle={s.status === 'pending'}>{s.label}</span>
              {#if s.message}
                <div class="msg" class:fail={s.status === 'failed'}>{s.message}</div>
              {/if}
              {#if s.status === 'running' && (s.key === 'online_copy' || s.key === 'offline_copy') && st.copy.percent > 0}
                <div class="bar"><div style="width: {st.copy.percent}%"></div></div>
                <div class="msg">{st.copy.percent}% · {st.copy.rate ?? ''} · {st.copy.eta ?? ''} left</div>
              {/if}
            </li>
          {/each}
        </ol>
        <p class="subtle small">Last update {ago(st.updatedAt)}</p>
        <button class="linkish" onclick={() => (showLog = !showLog)}>{showLog ? 'Hide' : 'Show'} log</button>
        {#if showLog && m.log}
          <pre class="log">{m.log.join('\n')}</pre>
        {/if}
      </Card>
    {/if}

    {#if !retired && !running}
      <Card title="Move this box to another machine">
        {#if !m.available}
          <p class="subtle">{m.reason}</p>
        {:else}
          <p class="muted small">
            Copies your data, apps and domain to a new Ubuntu machine, then retires this one. Your apps are stopped
            only during the final copy. You provide the new machine and delete this one afterwards.
          </p>

          <h3>1. Authorize this box on the new machine</h3>
          {#if m.key}
            <p class="small">
              On the new machine, create an account (e.g. <span class="mono">migration</span>) with passwordless sudo,
              and add this key to its <span class="mono">~/.ssh/authorized_keys</span>:
            </p>
            <div class="key">
              <code>{m.key}</code>
              <button onclick={copyKey}>{copied ? 'Copied' : 'Copy'}</button>
            </div>
            <p class="muted small">
              It also needs <span class="mono">rsync</span>, enough free disk, and no box installed on it yet.
            </p>
          {:else}
            <button onclick={createKey} disabled={busy !== ''}>{busy === 'key' ? 'Creating…' : 'Create migration key'}</button>
          {/if}

          {#if m.key}
            <h3>2. Check the new machine</h3>
            <div class="form">
              <label>
                Account and address
                <input placeholder="migration@203.0.113.9" bind:value={target} autocomplete="off" spellcheck="false" />
              </label>
              <button onclick={check} disabled={busy !== '' || !target.trim()}>
                {busy === 'preflight' ? 'Checking…' : 'Check'}
              </button>
            </div>
            <button class="linkish small" onclick={() => (showAdvanced = !showAdvanced)}>
              {showAdvanced ? 'Hide' : 'Show'} advanced
            </button>
            {#if showAdvanced}
              <label class="block small">
                Status URL <span class="subtle">(optional: an https address your provider gave you, which receives the progress)</span>
                <input placeholder="https://…" bind:value={statusUrl} autocomplete="off" spellcheck="false" />
              </label>
            {/if}

            {#if preflight}
              <ul class="checks">
                {#each preflight.checks as c (c.name)}
                  <li><Pill tone={checkTone(c.status)}>{c.status}</Pill> <span>{c.message}</span></li>
                {/each}
              </ul>
            {/if}

            <h3>3. Migrate</h3>
            <button class="primary" onclick={start} disabled={busy !== '' || !cleared}>
              {busy === 'start' ? 'Starting…' : 'Start migration'}
            </button>
            {#if !cleared}<span class="subtle small">Run the check first; every item must pass.</span>{/if}
          {/if}
        {/if}
        {#if actionError}<pre class="error pre">{actionError}</pre>{/if}
      </Card>
    {/if}
  {/if}
</div>

<style>
  .stack {
    display: grid;
    gap: 1rem;
  }
  h3 {
    font-size: 0.95rem;
    margin: 1.1rem 0 0.4rem;
  }
  .run-head {
    display: flex;
    gap: 0.5rem;
    align-items: center;
    flex-wrap: wrap;
    margin-bottom: 0.75rem;
  }
  .small {
    font-size: 0.8rem;
  }
  .steps,
  .checks {
    list-style: none;
    padding: 0;
    margin: 0 0 0.75rem;
    display: grid;
    gap: 0.3rem;
  }
  .steps li,
  .checks li {
    display: flex;
    gap: 0.5rem;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .steps li.group {
    margin-top: 0.5rem;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.03em;
  }
  .msg {
    flex-basis: 100%;
    margin-left: 2.2rem;
    font-size: 0.8rem;
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }
  .msg.fail {
    color: var(--bad-fg);
  }
  .bar {
    flex-basis: calc(100% - 2.2rem);
    margin-left: 2.2rem;
    height: 6px;
    border-radius: 3px;
    background: var(--surface-3);
    overflow: hidden;
  }
  .bar div {
    height: 100%;
    background: var(--primary);
  }
  .key {
    display: flex;
    gap: 0.5rem;
    align-items: flex-start;
  }
  .key code {
    flex: 1;
    min-width: 0;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    background: var(--surface-sunken);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.5rem;
    overflow-wrap: anywhere;
    user-select: all;
  }
  .form {
    display: flex;
    gap: 0.5rem;
    align-items: flex-end;
    flex-wrap: wrap;
  }
  .form label,
  .block {
    display: grid;
    gap: 0.25rem;
    font-size: 0.85rem;
  }
  .form label {
    flex: 1;
    min-width: 220px;
  }
  .block {
    margin-top: 0.5rem;
  }
  .linkish {
    border: none;
    background: none;
    padding: 0;
    color: var(--primary);
  }
  .linkish:hover:not(:disabled) {
    background: none;
    text-decoration: underline;
  }
  .pre {
    white-space: pre-wrap;
    font-family: inherit;
    margin: 0.75rem 0 0;
  }
  .log {
    margin: 0.6rem 0 0;
    max-height: 420px;
    overflow: auto;
    background: var(--surface-sunken);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 0.6rem;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
