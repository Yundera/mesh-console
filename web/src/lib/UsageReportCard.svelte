<script lang="ts">
  import Card from './Card.svelte'
  import Pill from './Pill.svelte'
  import { api, ApiError, type UsageReport } from './api'
  import { ago } from './format'

  let info = $state<UsageReport | null>(null)
  let error = $state('')
  let saving = $state(false)
  let result = $state<{ ok: boolean; message: string; output?: string } | null>(null)

  async function load() {
    try {
      info = await api.get<UsageReport>('/api/usage-report')
      error = ''
    } catch (e) {
      error = (e as Error).message
    }
  }
  $effect(() => {
    load()
  })

  async function set(value: 'on' | 'off') {
    result = null
    saving = true
    try {
      const res = await api.post<{ usageReport?: UsageReport }>('/api/usage-report', { value })
      if (res.usageReport) info = res.usageReport
      result = {
        ok: true,
        message: value === 'off' ? 'Done. This box no longer reports anything.' : 'Done. Reporting resumes with the next app opened.',
      }
    } catch (e) {
      const body = e instanceof ApiError ? (e.body as { result?: { output?: string } } | undefined) : undefined
      result = { ok: false, message: (e as Error).message, output: body?.result?.output }
    } finally {
      saving = false
    }
  }

  // Today's set is the newest day the agent keeps.
  const today = $derived(info?.agent?.days?.at(-1))
</script>

<Card title="Usage reporting">
  {#if !info}
    <p class={error ? 'error' : 'subtle'}>{error || 'Loading…'}</p>
  {:else}
    <p class="muted small">
      To count how many boxes use each app, this box tells {info.destination ? info.destination : 'its backend'}
      which apps were opened from outside it, once per day: the date and the app names. No addresses, pages, browsers
      or counts. You can turn it off; nothing else changes.
    </p>
    <div class="row">
      <Pill tone={info.setting === 'on' ? 'ok' : 'neutral'}>{info.setting === 'on' ? 'On' : 'Off'}</Pill>
      {#if info.editable}
        <button onclick={() => set(info?.setting === 'on' ? 'off' : 'on')} disabled={saving}>
          {saving ? 'Applying…' : info.setting === 'on' ? 'Turn off' : 'Turn on'}
        </button>
      {/if}
    </div>
    {#if !info.editable}
      <p class="small">Changing it from here is {info.blocked ?? 'disabled on this box'}.</p>
    {/if}

    {#if info.setting === 'on'}
      {#if !info.agent}
        <p class="subtle small">The router agent on this box does not report yet: it arrives with an update.</p>
      {:else}
        <p class="small">
          {#if today && today.apps.length}
            {today.date}: <span class="mono">{today.apps.join(', ')}</span>
            {#if today.pending}<span class="subtle">(not sent yet)</span>{/if}
          {:else}
            Nothing opened from outside today.
          {/if}
        </p>
        <p class="subtle small">
          Last sent {info.agent.lastSentAt ? ago(info.agent.lastSentAt) : 'never'}.
          {#if info.agent.lastError}<span class="warn-text">Last attempt failed: {info.agent.lastError}</span>{/if}
        </p>
      {/if}
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

<style>
  .row {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    margin: 0.75rem 0;
  }
  .small {
    font-size: 0.8rem;
  }
  .warn-text {
    color: var(--warn-fg);
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
