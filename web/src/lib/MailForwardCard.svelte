<script lang="ts">
  import Card from './Card.svelte'
  import Pill from './Pill.svelte'
  import { api, type MailForward } from './api'

  // Mail to <user>@<zone> forwarded to the owner's own inbox, after they confirm the
  // link the mesh server mails to that address (internal/server/mailforward.go).

  let fwd = $state<MailForward | null>(null)
  let error = $state('')
  let busy = $state('')
  let editing = $state(false)
  let target = $state('')
  let notice = $state('')

  async function load() {
    try {
      fwd = await api.get<MailForward>('/api/mail/forward')
      error = ''
    } catch (e) {
      error = (e as Error).message
    }
  }

  $effect(() => {
    load()
  })

  async function save(address: string) {
    busy = 'save'
    notice = ''
    error = ''
    try {
      fwd = await api.post<MailForward>('/api/mail/forward', { target: address.trim() })
      editing = false
      notice =
        fwd.state === 'pending'
          ? `We sent a confirmation link to ${fwd.target}. Mail is forwarded once you click it (the link is valid for 48 hours).`
          : ''
    } catch (e) {
      error = (e as Error).message
    } finally {
      busy = ''
    }
  }

  async function turnOff() {
    busy = 'off'
    notice = ''
    error = ''
    try {
      fwd = await api.delete<MailForward>('/api/mail/forward')
      editing = false
    } catch (e) {
      error = (e as Error).message
    } finally {
      busy = ''
    }
  }

  function startEdit() {
    target = fwd?.target ?? ''
    editing = true
    notice = ''
  }

  const user = $derived(fwd?.address?.split('@')[0] ?? '')
  const zone = $derived(fwd?.address?.split('@')[1] ?? '')
</script>

<Card title="Receive email">
  {#if !fwd}
    {#if error}<p class="error">{error}</p>{:else}<p class="subtle">Loading…</p>{/if}
  {:else if !fwd.supported}
    <p class="subtle">{fwd.message}</p>
  {:else if !fwd.enabled}
    <p class="subtle">{zone || 'This mesh server'} does not receive email, so there is nothing to forward.</p>
  {:else}
    <p>
      Email sent to <span class="mono addr">{fwd.address}</span> can be forwarded to an inbox you already use.
    </p>
    <p class="subtle small">
      <span class="mono">{user}+anything@{zone}</span> and your apps' addresses
      (<span class="mono">&lt;app&gt;.{user}@{zone}</span>) arrive there too. Replies go straight to the sender.
    </p>

    {#if fwd.state === 'active'}
      <p><Pill tone="ok">Forwarding</Pill> to <strong>{fwd.target}</strong></p>
    {:else if fwd.state === 'pending'}
      <p>
        <Pill tone="warn">Waiting for confirmation</Pill> Open the link sent to <strong>{fwd.target}</strong>. Nothing is
        forwarded until then.
      </p>
    {:else if fwd.state === 'paused'}
      <p><Pill tone="bad">Paused</Pill> {fwd.reason ?? 'mail to this address is not delivered'}.</p>
      <p class="subtle small">Set the address again to get a new confirmation link and resume.</p>
    {:else}
      <p><Pill>Off</Pill> Email to this address is not kept.</p>
    {/if}

    {#if editing || fwd.state === 'off'}
      <form class="form" onsubmit={(e) => { e.preventDefault(); save(target) }}>
        <label>
          Forward to
          <input type="email" placeholder="you@example.com" bind:value={target} autocomplete="email" spellcheck="false" />
        </label>
        <button class="primary" type="submit" disabled={busy !== '' || !target.trim()}>
          {busy === 'save' ? 'Sending link…' : 'Send confirmation link'}
        </button>
        {#if editing}<button type="button" onclick={() => (editing = false)} disabled={busy !== ''}>Cancel</button>{/if}
      </form>
    {:else}
      <div class="actions">
        {#if fwd.state === 'pending' || fwd.state === 'paused'}
          <button onclick={() => save(fwd!.target ?? '')} disabled={busy !== ''}>
            {busy === 'save' ? 'Sending…' : 'Send the link again'}
          </button>
        {/if}
        <button onclick={startEdit} disabled={busy !== ''}>Change address</button>
        <button onclick={turnOff} disabled={busy !== ''}>{busy === 'off' ? 'Turning off…' : 'Turn off'}</button>
      </div>
    {/if}

    {#if notice}<p class="ok-text small">{notice}</p>{/if}
    {#if error}<p class="error">{error}</p>{/if}
  {/if}
</Card>

<style>
  .addr {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .small {
    font-size: 0.8rem;
  }
  .form {
    display: flex;
    gap: 0.6rem;
    align-items: flex-end;
    flex-wrap: wrap;
  }
  .form label {
    display: grid;
    gap: 0.25rem;
    flex: 1 1 16rem;
    min-width: 0;
  }
  .actions {
    display: flex;
    gap: 0.6rem;
    flex-wrap: wrap;
  }
  .ok-text {
    color: var(--ok-fg);
  }
</style>
