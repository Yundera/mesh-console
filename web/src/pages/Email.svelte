<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import { api, type MailInfo, type MailEvent } from '../lib/api'
  import { ago, dateTime } from '../lib/format'

  let info = $state<MailInfo | null>(null)
  let error = $state('')
  let sending = $state(false)
  let testResult = $state<{ ok: boolean; message: string } | null>(null)
  let copied = $state('')

  async function load() {
    try {
      info = await api.get<MailInfo>('/api/mail')
      error = ''
    } catch (e) {
      error = (e as Error).message
    }
  }

  $effect(() => {
    load()
    const t = setInterval(load, 30_000)
    return () => clearInterval(t)
  })

  async function sendTest() {
    sending = true
    testResult = null
    try {
      const r = await api.post<{ to: string }>('/api/mail/test')
      testResult = { ok: true, message: `Sent to ${r.to}. It should arrive within a minute — check the spam folder too.` }
    } catch (e) {
      testResult = { ok: false, message: (e as Error).message }
    } finally {
      sending = false
      // The relay records the send; show it.
      setTimeout(load, 1500)
    }
  }

  async function copy(label: string, value: string) {
    try {
      await navigator.clipboard.writeText(value)
      copied = label
      setTimeout(() => (copied = ''), 1500)
    } catch {
      // Clipboard needs a secure context; the value is on screen anyway.
    }
  }

  const statusLabel: Record<MailEvent['status'], string> = {
    sent: 'Sent',
    skipped: 'Not delivered',
    failed: 'Failed',
    rate_limited: 'Over limit',
  }
  const statusTone = (s: MailEvent['status']) => (s === 'sent' ? 'ok' : s === 'failed' ? 'bad' : 'warn')

  const sender = $derived(
    info && info.domainName && info.serverDomain ? `${info.domainName}@${info.serverDomain}` : '',
  )
</script>

{#if error && !info}
  <Card><p class="error">{error}</p></Card>
{:else if !info}
  <Card><p class="subtle">Loading…</p></Card>
{:else}
  <div class="grid">
    <Card title="How your apps send email">
      <ol class="flow">
        <li><strong>Your apps</strong> <span class="subtle">hand their emails to the relay on this box</span></li>
        <li><strong>{info.serverDomain || 'The mesh server'}</strong> <span class="subtle">checks it really is your box</span></li>
        <li><strong>Delivered</strong> <span class="subtle">to the recipient's inbox</span></li>
      </ol>
      {#if sender}
        <p>
          Emails are sent as
          <span class="mono addr">&lt;app&gt;.{sender}</span>
        </p>
        <p class="subtle small">
          For example <span class="mono">vaultwarden.{sender}</span>. The part before the dot is the app's name,
          taken from the sender address the app is configured with; the rest is always your address, so no app can
          send in someone else's name.
        </p>
      {/if}
      <p class="subtle small">Up to {info.limitPerHour} emails per hour for this box.</p>
    </Card>

    <Card title="Connect an app">
      <p class="subtle small">Use these settings in any app that asks for an SMTP server.</p>
      <dl class="kv">
        <dt>Server</dt>
        <dd>
          <span class="mono">{info.setup.host}</span>
          <button class="copy" onclick={() => copy('host', info!.setup.host)}>{copied === 'host' ? 'Copied' : 'Copy'}</button>
        </dd>
        <dt>Port</dt>
        <dd>
          <span class="mono">{info.setup.port}</span>
          <button class="copy" onclick={() => copy('port', String(info!.setup.port))}>{copied === 'port' ? 'Copied' : 'Copy'}</button>
        </dd>
        <dt>Security</dt>
        <dd>None <span class="subtle small">(the connection never leaves this box)</span></dd>
        <dt>Login</dt>
        <dd>Not needed <span class="subtle small">(any username and password work)</span></dd>
        <dt>Sender</dt>
        <dd><span class="mono">&lt;appname&gt;@{info.serverDomain || 'example.com'}</span> <span class="subtle small">— only the name is kept</span></dd>
      </dl>
    </Card>

    <div class="wide">
      <Card title="Test">
        {#if info.accountEmail}
          <div class="test">
            <button class="primary" onclick={sendTest} disabled={sending}>{sending ? 'Sending…' : 'Send a test email'}</button>
            <span class="subtle">to <strong>{info.accountEmail}</strong>, your account email</span>
          </div>
          {#if testResult}
            <p class={testResult.ok ? 'ok-text' : 'error'}>{testResult.message}</p>
          {/if}
        {:else}
          <p class="subtle">No account email is set on this box, so there is nowhere to send a test.</p>
        {/if}
        {#if info.relayState && info.relayState !== 'running'}
          <p class="error">The mail relay is {info.relayState}. Apps cannot send email until it runs again.</p>
        {/if}
      </Card>
    </div>

    <div class="wide">
      <Card title="Activity">
        {#if info.statsError || !info.stats}
          <p class="subtle">{info.statsError ?? 'No activity reported.'}</p>
        {:else}
          {@const s = info.stats}
          <div class="kpis">
            <div class="kpi">
              <div class="n">{s.totals.h24.sent}</div>
              <div class="subtle small">sent today</div>
            </div>
            <div class="kpi">
              <div class="n">{s.totals.d7.sent}</div>
              <div class="subtle small">sent in 7 days</div>
            </div>
            <div class="kpi" class:bad={s.totals.d7.failed + s.totals.d7.rateLimited > 0}>
              <div class="n">{s.totals.d7.failed + s.totals.d7.rateLimited}</div>
              <div class="subtle small">failed in 7 days</div>
            </div>
            <div class="kpi" class:warn={s.totals.d7.skipped > 0}>
              <div class="n">{s.totals.d7.skipped}</div>
              <div class="subtle small">not delivered in 7 days</div>
            </div>
          </div>
          {#if s.totals.d7.skipped > 0}
            <p class="small warn-text">
              Some emails were accepted but not delivered: the mail service is not configured on
              {info.serverDomain || 'the server'}. Your apps are not at fault.
            </p>
          {/if}

          {#if s.apps.length}
            <h3>By app <span class="subtle small">(last {s.retentionDays} days)</span></h3>
            <div class="table-wrap">
              <table>
                <thead><tr><th>App</th><th>Sent as</th><th>Sent</th><th>Failed</th><th>Last</th></tr></thead>
                <tbody>
                  {#each s.apps as a (a.app)}
                    <tr>
                      <td>{a.app}</td>
                      <td class="mono small">{a.from ?? '—'}</td>
                      <td>{a.sent}</td>
                      <td>{a.failed + a.rateLimited + a.skipped || '—'}</td>
                      <td class="subtle">{ago(a.last)}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}

          {#if s.recent.length}
            <h3>Recent</h3>
            <div class="table-wrap">
              <table>
                <thead><tr><th>When</th><th>App</th><th>To</th><th>Result</th></tr></thead>
                <tbody>
                  {#each s.recent as e, i (i)}
                    <tr>
                      <td class="subtle" title={dateTime(e.time)}>{ago(e.time)}</td>
                      <td>{e.app}</td>
                      <td class="mono small">{e.to}</td>
                      <td>
                        <Pill tone={statusTone(e.status)}>{statusLabel[e.status] ?? e.status}</Pill>
                        {#if e.error}<div class="subtle small err">{e.error}</div>{/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {:else}
            <p class="subtle">No email has been sent yet.</p>
          {/if}
          {#if !s.persistent}
            <p class="subtle small">Activity is kept in memory only and resets when the relay restarts.</p>
          {/if}
        {/if}
      </Card>
    </div>
  </div>
{/if}

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
  .flow {
    margin: 0 0 1rem;
    padding-left: 1.2rem;
    display: grid;
    gap: 0.35rem;
  }
  .addr {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .small {
    font-size: 0.8rem;
  }
  .copy {
    padding: 0.1rem 0.5rem;
    font-size: 0.75rem;
    margin-left: 0.4rem;
  }
  .test {
    display: flex;
    gap: 0.75rem;
    align-items: center;
    flex-wrap: wrap;
  }
  .ok-text {
    color: var(--ok-fg);
  }
  .warn-text {
    color: var(--warn-fg);
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
  .kpi.bad .n {
    color: var(--bad-fg);
  }
  .kpi.warn .n {
    color: var(--warn-fg);
  }
  h3 {
    font-size: 0.85rem;
    margin: 1.2rem 0 0.6rem;
  }
  .err {
    max-width: 28rem;
    overflow-wrap: anywhere;
  }
</style>
