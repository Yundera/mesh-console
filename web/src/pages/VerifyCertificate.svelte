<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import { api, type CertificateVerify, type Holder, type SeenCert } from '../lib/api'
  import { dateTime } from '../lib/format'
  import { navigate } from '../lib/nav'

  type Browser = 'chrome' | 'edge' | 'firefox' | 'safari'

  // The address this page is open on, as the browser itself sees it: the
  // certificate to compare is the one the browser got for this exact name.
  const host = location.hostname

  let data = $state<CertificateVerify | null>(null)
  let error = $state('')
  let loading = $state(false)

  async function load() {
    loading = true
    try {
      data = await api.get<CertificateVerify>(`/api/certificates/verify?host=${encodeURIComponent(host)}`)
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

  const ua = navigator.userAgent
  const mobile = /Android|iPhone|iPad|iPod|Mobile/i.test(ua) || (navigator.maxTouchPoints > 1 && /Macintosh/.test(ua))
  let browser = $state<Browser>(
    /Edg\//.test(ua) ? 'edge' : /Firefox\//.test(ua) ? 'firefox' : /Chrome\/|Chromium\//.test(ua) ? 'chrome' : /Safari\//.test(ua) ? 'safari' : 'chrome',
  )
  const browsers: { id: Browser; label: string }[] = [
    { id: 'chrome', label: 'Chrome / Brave' },
    { id: 'edge', label: 'Edge' },
    { id: 'firefox', label: 'Firefox' },
    { id: 'safari', label: 'Safari (Mac)' },
  ]

  const holderLabel: Record<Holder, string> = {
    box: 'your box',
    cloudflare: 'Cloudflare',
    gateway: 'the mesh gateway',
    unknown: 'an unknown party',
  }

  const address = $derived(data?.address)
  const e2e = $derived(data?.endToEnd ?? [])
  // The address to send the person to when this one is not end to end: a dot name first.
  const target = $derived(e2e[0])
  // The one-liner checks the end-to-end address when there is one, else this one.
  const cmdHost = $derived(address?.endToEnd ? host : (target?.host ?? host))
  const command = $derived(
    `openssl s_client -connect ${cmdHost}:443 -servername ${cmdHost} </dev/null 2>/dev/null | openssl x509 -noout -subject -issuer -fingerprint -sha256`,
  )
  const ctUrl = $derived(data?.domain ? `https://crt.sh/?q=${encodeURIComponent('%.' + data.domain)}` : '')

  function pairs(fp: string): string[] {
    return fp.split(':')
  }

  let copied = $state('')
  async function copy(text: string, what: string) {
    try {
      await navigator.clipboard.writeText(text)
      copied = what
      setTimeout(() => (copied === what ? (copied = '') : null), 1500)
    } catch {
      copied = ''
    }
  }
</script>

{#snippet certificate(c: SeenCert)}
  <dl class="kv">
    <dt>Issued to</dt>
    <dd>
      <span class="mono strong">{c.issuedTo || '—'}</span>
      {#if c.dnsNames && c.dnsNames.length > 1}
        <span class="subtle small">also {c.dnsNames.filter((n) => n !== c.issuedTo).join(', ')}</span>
      {/if}
    </dd>
    <dt>Issued by</dt>
    <dd>{c.issuer || '—'}</dd>
    {#if c.notBefore && c.notAfter}
      <dt>Valid</dt>
      <dd>{dateTime(c.notBefore)} → {dateTime(c.notAfter)}</dd>
    {/if}
    <dt>SHA-256 fingerprint</dt>
    <dd>
      {#if c.fingerprint}
        <div class="fp mono" aria-label={c.fingerprint}>
          {#each pairs(c.fingerprint) as p, i}
            <span class:edge={i < 4 || i >= 28}>{p}</span>
          {/each}
        </div>
        <button class="small" onclick={() => copy(c.fingerprint!, c.host)}>{copied === c.host ? 'Copied' : 'Copy'}</button>
      {:else}
        —
      {/if}
    </dd>
  </dl>
{/snippet}

<div class="stack">
  <p class="small back">
    <a href="/certificates" onclick={(e) => navigate(e, '/certificates')}>← Certificates</a>
  </p>

  <Card title="Verify your certificate">
    {#snippet actions()}
      <button onclick={load} disabled={loading}>{loading ? 'Checking…' : 'Check again'}</button>
    {/snippet}
    <p class="intro">
      Your browser shows the certificate it received for this page. Compare it with the certificate your box reports
      below. If they are the same, your browser is talking to the party named here, and nobody in between can read the
      traffic.
    </p>

    <div class="head">
      <span class="subtle small">You are on</span>
      <span class="mono strong host">{host}</span>
      {#if address}
        {#if address.endToEnd}
          <Pill tone="ok">end to end</Pill>
        {:else}
          <Pill tone="info">decrypted by {holderLabel[address.holder]}</Pill>
        {/if}
      {/if}
    </div>

    {#if error && !data}
      <p class="error">{error}</p>
    {:else if !data}
      <p class="subtle">Asking your box which certificate it serves…</p>
    {:else}
      {#if data.addressError}
        <p class="small warn-text">{data.addressError}</p>
      {:else if address}
        {#if address.endToEnd}
          <p class="small">
            The key for this address is on <b>your box</b> only, so the certificate your browser shows should be exactly
            this one.
          </p>
        {:else}
          <p class="small">
            On this address, {holderLabel[address.holder]} decrypts the connection, then re-encrypts it to your box. Your
            browser therefore shows {holderLabel[address.holder]}'s certificate, not your box's. That is how this
            address works.
            {#if target}
              To check your box's own certificate, open this page on an end-to-end address:
            {:else}
              Your box has no end-to-end address that opens in a browser yet: once your domain is delegated (Domain
              page), its dot names are encrypted end to end.
            {/if}
          </p>
          {#if target}
            <p>
              <a class="cta" href={`https://${target.host}/certificates/verify`}>Open on {target.host} →</a>
            </p>
          {/if}
        {/if}
        {#if address.error && !address.fingerprint}
          <p class="small error">No certificate could be read: {address.error}</p>
        {:else}
          {#if !address.trusted}
            <p class="small warn-text">
              Browsers do not trust this certificate, so this address opens with a warning
              {#if address.error}({address.error}){/if}.
            </p>
          {/if}
          {@render certificate(address)}
        {/if}
      {/if}
      <p class="subtle small">Checked {dateTime(data.checkedAt)}.</p>
      {#if error}<p class="error small">{error}</p>{/if}
    {/if}
  </Card>

  <Card title="Find it in your browser">
    {#if mobile}
      <p class="small warn-text">
        Phone browsers do not show certificate details (Safari on iPhone and iPad not at all). Do this check on a computer.
      </p>
    {/if}
    <div class="tabs" role="tablist">
      {#each browsers as b (b.id)}
        <button role="tab" aria-selected={browser === b.id} class:active={browser === b.id} onclick={() => (browser = b.id)}>
          {b.label}
        </button>
      {/each}
    </div>
    <ol class="steps">
      {#if browser === 'chrome'}
        <li>Click the icon at the left of the address bar (two small sliders; older versions show a padlock).</li>
        <li>Click <b>Connection is secure</b>, then <b>Certificate is valid</b>.</li>
        <li>
          On the <b>General</b> tab, read <b>Issued To › Common Name</b> and, under <b>SHA-256 Fingerprints</b>, the
          <b>Certificate</b> line (not the <i>Public key</i> line).
        </li>
      {:else if browser === 'edge'}
        <li>Click the padlock at the left of the address bar.</li>
        <li>Click <b>Connection is secure</b>, then the certificate icon at the top right of the panel.</li>
        <li>
          On the <b>General</b> tab, read <b>Issued To › Common Name</b> and, under <b>SHA-256 Fingerprints</b>, the
          <b>Certificate</b> line (not the <i>Public key</i> line).
        </li>
      {:else if browser === 'firefox'}
        <li>Click the padlock at the left of the address bar.</li>
        <li>Click <b>Connection secure</b>, then <b>More information</b>, then <b>View Certificate</b>.</li>
        <li>
          In the first tab (the name of your address), read <b>Subject Name › Common Name</b> and
          <b>Fingerprints › SHA-256</b>.
        </li>
      {:else}
        <li>Click the padlock in the address bar.</li>
        <li>Click <b>Show Certificate</b>, then open <b>Details</b>.</li>
        <li>Read <b>Subject Name › Common Name</b>, then scroll to <b>Fingerprints › SHA-256</b>.</li>
      {/if}
      <li>
        Compare with the certificate above. Browsers write the fingerprint with spaces or colons, in capitals or not: only
        the letters and digits count. Comparing the first and last groups (in bold above) is enough.
      </li>
    </ol>
  </Card>

  <Card title="What the result means">
    <ul class="meaning">
      <li>
        <Pill tone="ok">Same</Pill>
        <span>Your browser received the certificate shown above. On an end-to-end address, only your box can decrypt the connection.</span>
      </li>
      <li>
        <Pill tone="info">Different, on a dash address</Pill>
        <span>Expected: those addresses are decrypted by the mesh. Use an end-to-end address to check your box.</span>
      </li>
      <li>
        <Pill tone="warn">Different, on an end-to-end address</Pill>
        <span>
          First click <b>Check again</b> and reload: your box may just have renewed its certificate. If it still differs,
          something between your computer and your box shows its own certificate. Most often that is an antivirus or a
          company network that inspects encrypted traffic (its name then appears under <i>Issued by</i>). Otherwise,
          treat it as an interception: do not sign in, and check from another network.
        </span>
      </li>
    </ul>
    <p class="subtle small">
      Your box renews its certificate about every two months, so the fingerprint changes then. This page always reads the
      current one.
    </p>
  </Card>

  <Card title="Check it without this page">
    <p class="small intro">
      This page reaches you over the connection being checked, so someone able to intercept that connection could also
      alter what it shows. For a check that does not depend on it:
    </p>
    {#if ctUrl}
      <h3>Public certificate logs</h3>
      <p class="small">
        Every trusted certificate is recorded in public Certificate Transparency logs.
        <a href={ctUrl} target="_blank" rel="noopener">See all certificates ever issued for {data?.domain}</a>. They should
        all be from Let's Encrypt, about one every two months. One from another authority, or issued when your box did not
        renew, is worth asking about. Your domain's DNS also allows only your box's Let's Encrypt accounts to obtain one
        (CAA, on the Certificates page).
      </p>
    {/if}
    <h3>From a terminal</h3>
    <p class="small">On any computer with OpenSSL, preferably on another network than this one:</p>
    <div class="cmd">
      <pre>{command}</pre>
      <button class="small" onclick={() => copy(command, 'cmd')}>{copied === 'cmd' ? 'Copied' : 'Copy'}</button>
    </div>
    <p class="subtle small">It prints the same name and SHA-256 fingerprint as above.</p>
  </Card>
</div>

<style>
  .stack {
    display: grid;
    gap: 1rem;
  }
  .back {
    margin: 0;
  }
  .intro {
    margin-top: 0;
    max-width: 52rem;
  }
  .small {
    font-size: 0.8rem;
  }
  .strong {
    font-weight: 600;
  }
  .host {
    overflow-wrap: anywhere;
  }
  .warn-text {
    color: var(--warn-fg);
  }
  h3 {
    font-size: 0.9rem;
    margin: 1rem 0 0.3rem;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.4rem 0.6rem;
    margin: 0.6rem 0 0.2rem;
  }
  .cta {
    display: inline-block;
    padding: 0.4rem 0.8rem;
    border-radius: 8px;
    background: var(--surface-3);
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .kv {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 0.45rem 1rem;
    margin: 0.8rem 0 0.4rem;
    font-size: 0.85rem;
  }
  .kv dt {
    color: var(--text-muted);
  }
  .kv dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .fp {
    display: grid;
    grid-template-columns: repeat(8, max-content);
    gap: 0.15rem 0.45rem;
    margin-bottom: 0.4rem;
    font-size: 0.85rem;
    color: var(--text-muted);
  }
  .fp .edge {
    color: var(--text);
    font-weight: 700;
  }
  .tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
    margin-bottom: 0.6rem;
  }
  .tabs button {
    background: transparent;
    border: 1px solid var(--border);
  }
  .tabs button.active {
    background: var(--surface-3);
    font-weight: 600;
  }
  .steps {
    margin: 0;
    padding-left: 1.3rem;
    display: grid;
    gap: 0.45rem;
    font-size: 0.9rem;
    max-width: 52rem;
  }
  .steps b,
  .meaning b,
  .intro b {
    font-weight: 600;
  }
  .meaning {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.7rem;
    font-size: 0.9rem;
  }
  .meaning li {
    display: grid;
    gap: 0.25rem;
    justify-items: start;
  }
  .cmd {
    display: flex;
    gap: 0.5rem;
    align-items: flex-start;
  }
  pre {
    flex: 1;
    min-width: 0;
    margin: 0;
    padding: 0.5rem 0.7rem;
    background: var(--surface-2);
    border-radius: 6px;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  @media (max-width: 520px) {
    .kv {
      grid-template-columns: minmax(0, 1fr);
      gap: 0.1rem;
    }
    .kv dd {
      margin-bottom: 0.5rem;
    }
    .fp {
      grid-template-columns: repeat(4, max-content);
    }
  }
</style>
