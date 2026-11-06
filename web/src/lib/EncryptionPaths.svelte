<script lang="ts">
  // One lane per address a browser opens without a warning: where each TLS
  // session starts and ends, which CA signed the certificate that ends it, and
  // who holds that certificate's key. Built by lib/encryption.ts.
  import Pill from './Pill.svelte'
  import type { Path, Verdict, Node } from './encryption'
  import type { OutsideProbe } from './api'

  let { paths }: { paths: Path[] } = $props()

  const verdictTone: Record<Verdict, 'ok' | 'warn' | 'bad' | 'neutral'> = {
    e2e: 'ok',
    split: 'warn',
    broken: 'bad',
    off: 'neutral',
  }

  function seenText(o: OutsideProbe): string {
    if (!o.holder) return `No answer${o.error ? ': ' + o.error : ''}`
    const who = o.holder === 'cloudflare' ? 'Cloudflare' : o.holder === 'gateway' ? 'the mesh gateway' : o.holder === 'box' ? 'your box' : 'unknown'
    return `Checked from the internet: certificate held by ${who}${o.issuer ? ', issued by ' + o.issuer : ''}${o.verified ? '' : ', refused by a browser'}`
  }
</script>

{#snippet node(n: Node)}
  <div class="node" class:box={n.box} class:reads={n.reads && !n.box}>
    <span class="node-name">
      {#if n.reads}
        <svg class="lock" viewBox="0 0 16 16" aria-hidden="true"
          ><rect x="3" y="7" width="10" height="7" rx="1.5" /><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0" /></svg
        >
      {/if}
      {n.name}
    </span>
    <span class="node-sub">{n.box ? 'decrypts' : n.reads ? 'decrypts: can read' : 'encrypts'}</span>
  </div>
{/snippet}

<ul class="lanes">
  {#each paths as p (p.form)}
    <li class="lane" class:off={p.verdict === 'off'}>
      <div class="head">
        <span class="form">{p.label}</span>
        {#if p.host.includes('<')}
          <span class="mono host">{p.host}</span>
        {:else}
          <a class="mono host" href={`https://${p.host}/`} target="_blank" rel="noopener">{p.host}</a>
        {/if}
        <Pill tone={verdictTone[p.verdict]}>{p.verdictText}</Pill>
      </div>

      <div class="chain">
        {@render node({ name: 'Browser', reads: false })}
        {#each p.legs as { leg, to }, i (i)}
          <div class="leg {leg.kind}">
            <div class="bar" class:tunnel={leg.tunnel}>
              {#each leg.through ?? [] as t}
                <span class="through">{t} · passes it on, cannot read</span>
              {/each}
            </div>
            <div class="leg-label">
              {#each leg.through ?? [] as t}
                <span class="through-inline">{t} · passes it on, cannot read</span>
              {/each}
              <span class="ca">{leg.ca}</span>
              <span class="mono detail">{leg.detail}</span>
              {#if leg.tunnel}<span class="detail">inside the WireGuard tunnel</span>{/if}
              {#if leg.check}<span class="detail check">{leg.check}</span>{/if}
            </div>
          </div>
          {@render node(to)}
        {/each}
      </div>

      {#if p.seen}
        <p class="subtle small seen" class:bad={!p.seen.verified}>{seenText(p.seen)}</p>
      {/if}
      {#each p.notes as n}
        <p class="subtle small note">{n}</p>
      {/each}
    </li>
  {/each}
</ul>

<div class="legend small">
  <div><span class="swatch box"></span><b>Let's Encrypt, key on your box.</b> A browser trusts it, and only your box can decrypt.</div>
  <div>
    <span class="swatch operator"></span><b>Key held by the mesh</b> (Cloudflare or the mesh gateway). A browser trusts it; the
    mesh decrypts there. For your dot names the gateway holds a fallback certificate, used only while your box has none of
    its own, so they never fail in the browser.
  </div>
  <div>
    <span class="swatch mesh"></span><b>Mesh Router CA (cert0).</b> A private CA only the mesh's own machines trust. It secures
    the leg from the mesh to your box and is never shown to a browser. IP names and the box name are this leg, not addresses
    to open.
  </div>
  <div><span class="swatch tunnel"></span><b>Inside the WireGuard tunnel</b>, for a box without a public IP.</div>
</div>

<style>
  .lanes {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .lane {
    padding: 0.9rem 0;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }
  .lane:first-child {
    padding-top: 0;
  }
  .lane.off .chain {
    opacity: 0.45;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.4rem 0.6rem;
    margin-bottom: 0.6rem;
  }
  .form {
    font-weight: 600;
  }
  .host {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .small {
    font-size: 0.8rem;
  }
  .note,
  .seen {
    margin: 0.35rem 0 0;
    max-width: 52rem;
  }
  .seen.bad {
    color: var(--bad-fg);
  }

  /* ---- the chain: node, leg, node, … ---- */
  .chain {
    display: flex;
    align-items: flex-start;
    gap: 0;
    min-width: 0;
  }
  .node {
    flex: 0 0 auto;
    display: flex;
    flex-direction: column;
    align-items: center;
    min-width: 6.5rem;
    padding: 0.4rem 0.6rem;
    border: 1px solid var(--border-strong);
    border-radius: 8px;
    background: var(--surface-2);
    text-align: center;
  }
  .node.box {
    border-color: var(--ok-fg);
    background: var(--ok-bg);
  }
  .node.reads {
    border-color: var(--warn-fg);
    background: var(--warn-bg);
  }
  .node-name {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    font-weight: 600;
    font-size: 0.85rem;
    white-space: nowrap;
  }
  .node-sub {
    font-size: 0.7rem;
    color: var(--text-muted);
  }
  .lock {
    width: 0.85rem;
    height: 0.85rem;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.5;
  }
  .leg {
    flex: 1 1 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    padding-top: 1.05rem;
  }
  .bar {
    position: relative;
    height: 6px;
    border-radius: 3px;
    background: var(--leg);
    display: flex;
    justify-content: center;
  }
  .bar.tunnel {
    outline: 2px solid var(--leg);
    outline-offset: 3px;
  }
  .leg.box {
    --leg: var(--ok-fg);
  }
  .leg.operator {
    --leg: var(--warn-fg);
  }
  .leg.mesh {
    --leg: var(--info-fg);
  }
  .leg.mesh .bar {
    background: repeating-linear-gradient(90deg, var(--leg) 0 8px, transparent 8px 13px);
  }
  .through {
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
    padding: 0.1rem 0.45rem;
    border: 1px dashed var(--border-strong);
    border-radius: 6px;
    background: var(--surface);
    font-size: 0.7rem;
    color: var(--text-muted);
    white-space: nowrap;
    max-width: 95%;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .through-inline {
    display: none;
  }
  .leg-label {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.1rem;
    padding: 0.55rem 0.5rem 0;
    text-align: center;
    min-width: 0;
  }
  .ca {
    font-weight: 600;
    font-size: 0.8rem;
    color: var(--leg);
  }
  .detail {
    font-size: 0.72rem;
    color: var(--text-muted);
    overflow-wrap: anywhere;
  }
  .check {
    font-style: italic;
  }

  @media (max-width: 720px) {
    .chain {
      flex-direction: column;
      align-items: stretch;
    }
    .node {
      align-items: flex-start;
      text-align: left;
    }
    .leg {
      flex: 0 0 auto;
      flex-direction: row;
      padding: 0 0 0 1.2rem;
      min-height: 3.2rem;
    }
    .bar {
      width: 6px;
      height: auto;
      flex: 0 0 6px;
      flex-direction: column;
      justify-content: center;
    }
    .leg.mesh .bar {
      background: repeating-linear-gradient(180deg, var(--leg) 0 8px, transparent 8px 13px);
    }
    .through {
      display: none;
    }
    .through-inline {
      display: inline-block;
      margin-bottom: 0.2rem;
      padding: 0.1rem 0.45rem;
      border: 1px dashed var(--border-strong);
      border-radius: 6px;
      font-size: 0.7rem;
      color: var(--text-muted);
    }
    .leg-label {
      align-items: flex-start;
      text-align: left;
      padding: 0.5rem 0 0.5rem 0.7rem;
    }
  }

  .legend {
    display: grid;
    gap: 0.35rem;
    margin-top: 0.9rem;
    padding: 0.7rem 0.8rem;
    background: var(--surface-2);
    border-radius: 8px;
    color: var(--text-muted);
  }
  .legend b {
    color: var(--text);
  }
  .swatch {
    display: inline-block;
    width: 1.6rem;
    height: 6px;
    border-radius: 3px;
    margin-right: 0.5rem;
    vertical-align: middle;
  }
  .swatch.box {
    background: var(--ok-fg);
  }
  .swatch.operator {
    background: var(--warn-fg);
  }
  .swatch.mesh {
    background: repeating-linear-gradient(90deg, var(--info-fg) 0 6px, transparent 6px 10px);
  }
  .swatch.tunnel {
    background: var(--text-muted);
    outline: 2px solid var(--text-muted);
    outline-offset: 2px;
  }
</style>
