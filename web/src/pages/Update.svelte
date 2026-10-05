<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import { api, ApiError, type UpdateInfo, type SelfCheck, type Run, type Channel } from '../lib/api'
  import { ago, dateTime, duration, shortSha } from '../lib/format'

  let info = $state<UpdateInfo | null>(null)
  let infoError = $state('')
  let sc = $state<SelfCheck | null>(null)
  // Set while polls fail right after starting an update: the self-check
  // recreates this very console, so a few failed requests are expected.
  let unreachableSince = $state<number | null>(null)
  let starting = $state(false)
  let startError = $state('')
  let showLog = $state(false)
  let refreshing = $state(false)

  // Channel picker. `channel` is the selection, seeded from the box once.
  let channel = $state<Channel | null>(null)
  let customUrl = $state('')
  let savingChannel = $state(false)
  let channelResult = $state<{ ok: boolean; message: string; output?: string } | null>(null)

  const CHANNELS: { id: Channel; label: string; description: string }[] = [
    { id: 'stable', label: 'Stable', description: 'Tested releases. Recommended.' },
    { id: 'dev', label: 'Development', description: 'The main branch: latest features, less tested.' },
    { id: 'local', label: 'Local', description: 'Keep the installed template. The self-check still repairs the box but never downloads.' },
    { id: 'custom', label: 'Custom', description: 'Any template tarball (.tar.gz) — a fork, a test branch, or a file:// path on the host.' },
  ]

  async function loadInfo(refresh = false) {
    refreshing = refresh
    try {
      info = await api.get<UpdateInfo>('/api/update' + (refresh ? '?refresh=1' : ''))
      infoError = ''
      if (channel === null) {
        channel = info.channel
        if (info.channel === 'custom') customUrl = info.updateUrl
      }
    } catch (e) {
      infoError = (e as Error).message
    } finally {
      refreshing = false
    }
  }

  async function loadSelfCheck() {
    try {
      sc = await api.get<SelfCheck>('/api/selfcheck?lines=300')
      if (unreachableSince !== null) {
        unreachableSince = null
        loadInfo() // back after a restart: the marker may have moved
      }
    } catch (e) {
      if (running && unreachableSince === null) unreachableSince = Date.now()
      if (!running) sc = { error: (e as Error).message }
    }
  }

  const running = $derived(!!sc?.running || starting)
  const lastRun = $derived<Run | undefined>(sc?.runs?.length ? sc.runs[sc.runs.length - 1] : undefined)
  const previousRun = $derived<Run | undefined>(
    sc?.runs && sc.runs.length > 1 ? sc.runs[sc.runs.length - 2] : undefined,
  )

  $effect(() => {
    loadInfo()
    loadSelfCheck()
  })

  // Poll fast while a run is going, slowly otherwise.
  $effect(() => {
    const t = setInterval(loadSelfCheck, running || unreachableSince !== null ? 3000 : 30_000)
    return () => clearInterval(t)
  })

  let wasRunning = false
  $effect(() => {
    if (wasRunning && !running) loadInfo()
    wasRunning = running
  })

  async function runUpdate() {
    startError = ''
    starting = true
    try {
      await api.post('/api/update/run')
      // Give the runner a moment to write its first log line.
      setTimeout(loadSelfCheck, 1500)
      setTimeout(() => (starting = false), 6000)
    } catch (e) {
      starting = false
      startError = e instanceof ApiError && e.status === 409 ? 'An update or another host action is already running.' : (e as Error).message
    }
  }

  const head = (id: Channel) => info?.channels?.find((c) => c.id === id)
  const channelChanged = $derived(
    !!info &&
      channel !== null &&
      (channel !== info.channel || (channel === 'custom' && customUrl.trim() !== info.updateUrl)),
  )

  async function saveChannel(run: boolean) {
    if (!channel) return
    channelResult = null
    savingChannel = true
    try {
      const res = await api.post<{ started: boolean; startError?: string }>('/api/update/channel', {
        channel,
        url: channel === 'custom' ? customUrl.trim() : '',
        run,
      })
      channelResult = res.startError
        ? { ok: false, message: `Channel saved, but the update did not start: ${res.startError}` }
        : {
            ok: true,
            message: res.started
              ? 'Channel saved. Update started — progress below.'
              : 'Channel saved. The next self-check (nightly, or Update now) uses it.',
          }
      if (res.started) {
        starting = true
        setTimeout(loadSelfCheck, 1500)
        setTimeout(() => (starting = false), 6000)
      }
      channel = null // reseed from the box
      await loadInfo()
    } catch (e) {
      const body = e instanceof ApiError ? (e.body as { result?: { output?: string } } | undefined) : undefined
      channelResult = { ok: false, message: (e as Error).message, output: body?.result?.output }
    } finally {
      savingChannel = false
    }
  }

  const stateTone = (s: UpdateInfo['state']) => (s === 'up-to-date' ? 'ok' : s === 'outdated' ? 'warn' : 'neutral')
  const stateLabel = (s: UpdateInfo['state']) => (s === 'up-to-date' ? 'Up to date' : s === 'outdated' ? 'Update available' : 'Unknown')
  const runTone = (s: Run['status']) => (s === 'success' ? 'ok' : s === 'failed' ? 'bad' : s === 'interrupted' ? 'warn' : 'info')
  const stepTone = (s: string) => (s === 'success' ? 'ok' : s === 'failed' ? 'bad' : 'info')
</script>

<div class="stack">
  <Card title="Template version">
    {#snippet actions()}
      <button onclick={() => loadInfo(true)} disabled={refreshing}>{refreshing ? 'Checking…' : 'Check now'}</button>
    {/snippet}
    {#if infoError}
      <p class="error">{infoError}</p>
    {:else if !info}
      <p class="subtle">Loading…</p>
    {:else}
      <div class="headline">
        <Pill tone={stateTone(info.state)}>{stateLabel(info.state)}</Pill>
        {#if !info.autoUpdate}<Pill tone="warn">auto-update off</Pill>{/if}
      </div>
      <dl class="kv">
        <dt>Installed</dt>
        <dd>
          {#if info.installed?.commit}
            <span class="mono">{shortSha(info.installed.commit)}</span>
            <span class="subtle">synced {ago(info.installed.synced_at)}</span>
          {:else if info.installed}
            <span class="subtle">commit not recorded · synced {ago(info.installed.synced_at)}</span>
          {:else}
            <span class="subtle">
              not recorded yet{info.templateSyncedAt ? ` · template last synced ${ago(info.templateSyncedAt)}` : ''} — the
              next self-check records it
            </span>
          {/if}
        </dd>
        <dt>Latest</dt>
        <dd>
          {#if info.latest}
            <span class="mono">{shortSha(info.latest.commit)}</span>
            {#if info.latest.message}<span>{info.latest.message}</span>{/if}
            <span class="subtle">{ago(info.latest.date)}</span>
          {:else}
            <span class="subtle">{info.latestError ?? '—'}</span>
          {/if}
        </dd>
        <dt>Source</dt>
        <dd>
          <Pill tone="neutral">{CHANNELS.find((c) => c.id === info?.channel)?.label ?? info.channel}</Pill>
          {#if info.repo}<span class="mono">{info.repo}</span>{:else}<span class="mono">{info.updateUrl}</span>{/if}
        </dd>
        <dt>Nightly check</dt>
        <dd>{info.cron === 'disabled' || info.cron === 'off' ? 'disabled' : `cron ${info.cron || '0 3 * * *'}`}</dd>
      </dl>
      {#if !info.autoUpdate}
        <p class="muted small">
          MESH_AUTO_UPDATE is off: running the self-check repairs and restarts the stack but does not download a new
          template.
        </p>
      {/if}
    {/if}
  </Card>

  <Card title="Update channel">
    <p class="muted small">
      Where this box takes its template from. Saving only changes the source; the box moves to it at the next
      self-check — run one now with <em>Save &amp; update now</em>.
    </p>
    {#if !info || channel === null}
      <p class="subtle">Loading…</p>
    {:else}
      {#if !info.channelEditable}
        <p class="small">Changing the channel from here is {info.channelBlocked ?? 'disabled on this box'}.</p>
      {/if}
      <div class="channels" role="radiogroup" aria-label="Update channel">
        {#each CHANNELS as c (c.id)}
          {@const h = head(c.id)}
          <label class="channel" class:selected={channel === c.id}>
            <input
              type="radio"
              name="channel"
              value={c.id}
              bind:group={channel}
              disabled={!info.channelEditable || savingChannel}
            />
            <span class="body">
              <span class="name">
                {c.label}
                {#if info.channel === c.id}<Pill tone="ok">current</Pill>{/if}
              </span>
              <span class="muted small">{c.description}</span>
              {#if h?.latest}
                <span class="subtle small">
                  head <span class="mono">{shortSha(h.latest.commit)}</span>{h.latest.message
                    ? ` · ${h.latest.message}`
                    : ''}{h.latest.date ? ` · ${ago(h.latest.date)}` : ''}
                </span>
              {:else if h?.latestError}
                <span class="subtle small">head unknown: {h.latestError}</span>
              {/if}
            </span>
          </label>
        {/each}
      </div>
      {#if channel === 'custom'}
        <label class="url">
          <span>Template URL</span>
          <input
            type="url"
            bind:value={customUrl}
            placeholder="https://github.com/<owner>/mesh-router-template-root/archive/refs/heads/<branch>.tar.gz"
            disabled={!info.channelEditable || savingChannel}
          />
        </label>
      {/if}
      {#if info.channelEditable}
        <div class="channel-actions">
          <button
            onclick={() => saveChannel(false)}
            disabled={!channelChanged || savingChannel || running || (channel === 'custom' && !customUrl.trim())}
          >
            Save
          </button>
          <button
            class="primary"
            onclick={() => saveChannel(true)}
            disabled={!channelChanged || savingChannel || running || (channel === 'custom' && !customUrl.trim())}
          >
            {savingChannel ? 'Saving…' : 'Save & update now'}
          </button>
        </div>
      {/if}
      {#if channelResult}
        <p>
          <Pill tone={channelResult.ok ? 'ok' : 'bad'}>{channelResult.ok ? 'done' : 'problem'}</Pill>
          {channelResult.message}
        </p>
        {#if channelResult.output}<pre class="log">{channelResult.output}</pre>{/if}
      {/if}
    {/if}
  </Card>

  <Card title="Self-check">
    {#snippet actions()}
      <button class="primary" onclick={runUpdate} disabled={running || info?.windowsMode}>
        {running ? 'Running…' : 'Update now'}
      </button>
    {/snippet}
    <p class="muted small">
      Runs the same self-check as the nightly job: syncs the template, applies migrations, pulls images and brings every
      stack up. Services — this console included — may restart while it runs.
    </p>
    {#if startError}<p class="error">{startError}</p>{/if}
    {#if unreachableSince !== null}
      <p><Pill tone="info">console restarting</Pill> <span class="subtle">reconnecting…</span></p>
    {/if}

    {#if sc?.error && !sc.runs}
      <p class="error">{sc.error}</p>
    {:else if lastRun}
      <div class="run-head">
        <Pill tone={running ? 'info' : runTone(lastRun.status)}>
          {running ? 'running' : lastRun.status === 'incomplete' ? 'stalled' : lastRun.status}
        </Pill>
        <span>started {dateTime(lastRun.started)}</span>
        {#if lastRun.ended}<span class="subtle">· finished {ago(lastRun.ended)}</span>{/if}
      </div>
      <ol class="steps">
        {#each lastRun.steps ?? [] as s, i (i)}
          <li>
            <Pill tone={stepTone(s.status)}>{s.status === 'running' ? '…' : s.status === 'success' ? '✓' : '✗'}</Pill>
            <span class="mono">{s.name}</span>
            <span class="subtle">{duration(s.durationS)}</span>
            {#if s.status === 'failed'}
              <div class="fail">exit {s.exitCode ?? '?'}{s.lastOutput ? ` — ${s.lastOutput}` : ''}</div>
            {/if}
          </li>
        {/each}
      </ol>
      {#if previousRun && !running}
        <p class="subtle small">Previous run: {previousRun.status} · {dateTime(previousRun.started)}</p>
      {/if}
    {:else}
      <p class="subtle">No self-check run in the log yet.</p>
    {/if}

    <button class="linkish" onclick={() => (showLog = !showLog)}>{showLog ? 'Hide' : 'Show'} raw log</button>
    {#if showLog && sc?.log}
      <pre class="log">{sc.log.join('\n')}</pre>
    {/if}
  </Card>
</div>

<style>
  .stack {
    display: grid;
    gap: 1rem;
  }
  .headline,
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
  .steps {
    list-style: none;
    padding: 0;
    margin: 0 0 0.75rem;
    display: grid;
    gap: 0.3rem;
  }
  .steps li {
    display: flex;
    gap: 0.5rem;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .fail {
    flex-basis: 100%;
    margin-left: 2.2rem;
    color: var(--bad-fg);
    font-size: 0.8rem;
    overflow-wrap: anywhere;
  }
  .channels {
    display: grid;
    gap: 0.5rem;
    grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr));
    margin: 0.75rem 0;
  }
  .channel {
    display: flex;
    gap: 0.6rem;
    align-items: flex-start;
    padding: 0.65rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: 8px;
    cursor: pointer;
    min-width: 0;
  }
  .channel.selected {
    border-color: var(--primary);
    box-shadow: inset 0 0 0 1px var(--primary);
  }
  .channel input {
    margin-top: 0.2rem;
    padding: 0;
  }
  .channel .body {
    display: grid;
    gap: 0.2rem;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .channel .name {
    display: flex;
    gap: 0.4rem;
    align-items: center;
    font-weight: 600;
  }
  .url {
    display: grid;
    gap: 0.25rem;
    margin-bottom: 0.75rem;
  }
  .url span {
    font-size: 0.75rem;
    color: var(--text-muted);
  }
  .url input {
    width: 100%;
    box-sizing: border-box;
  }
  .channel-actions {
    display: flex;
    gap: 0.5rem;
    justify-content: flex-end;
    flex-wrap: wrap;
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
