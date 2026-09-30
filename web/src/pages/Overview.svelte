<script lang="ts">
  import Card from '../lib/Card.svelte'
  import Pill from '../lib/Pill.svelte'
  import { api, type Status, type Level } from '../lib/api'
  import { ago } from '../lib/format'
  import { navigate } from '../lib/nav'

  let status = $state<Status | null>(null)
  let error = $state('')

  async function load() {
    try {
      status = await api.get<Status>('/api/status')
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

  const headline: Record<Level, string> = {
    ok: 'All good',
    info: 'All good',
    warn: 'Needs attention',
    bad: 'Not working',
    unknown: 'Checking…',
  }

  const tiles = [
    { key: 'reachability', title: 'Internet access' },
    { key: 'updates', title: 'Updates' },
    { key: 'services', title: 'Services' },
    { key: 'email', title: 'Email' },
  ] as const

  const tone = (l: Level) => (l === 'unknown' ? 'neutral' : l)

  // One sentence under the headline, built from the tiles.
  function summaryLine(s: Status['summary']): string {
    const t = s.tiles
    const parts = [
      t.reachability.level === 'ok' ? 'Reachable from the internet' : t.reachability.label,
      t.updates.label,
      t.services.level === 'ok' ? `${t.services.detail ?? ''} running`.trim() : t.services.label,
    ]
    return parts.filter(Boolean).join(' · ')
  }
</script>

{#if error && !status}
  <Card><p class="error">{error}</p></Card>
{:else if !status}
  <Card><p class="subtle">Loading…</p></Card>
{:else}
  <section class="hero {status.summary.level}">
    <div class="identity">
      <a class="domain" href={status.links.root} target="_blank" rel="noopener">{status.domain}</a>
      <div class="ip">
        Public IP <span class="mono">{status.publicIp || '—'}</span>
        {#if status.publicIpv6 && status.publicIpv6 !== status.publicIp}
          · <span class="mono">{status.publicIpv6}</span>
        {/if}
      </div>
    </div>

    <div class="verdict">
      <span class="dot" aria-hidden="true"></span>
      <div>
        <div class="headline">{headline[status.summary.level]}</div>
        <div class="line">{summaryLine(status.summary)}</div>
      </div>
    </div>

    <div class="buttons">
      <a class="btn primary" href={status.links.maison} target="_blank" rel="noopener">Open Maison</a>
      {#if status.links.dashboard}
        <a class="btn" href={status.links.dashboard} target="_blank" rel="noopener">Account dashboard</a>
      {/if}
    </div>
    {#if error}<p class="error small">Refresh failed: {error}</p>{/if}
  </section>

  {#if status.summary.issues.length}
    <Card title="What needs attention">
      <ul class="issues">
        {#each status.summary.issues as i, n (n)}
          <li>
            <Pill tone={tone(i.level)}>{i.level === 'bad' ? 'Problem' : 'Check'}</Pill>
            <span>{i.message}</span>
            <a href={i.link} onclick={(e) => navigate(e, i.link)}>Details</a>
          </li>
        {/each}
      </ul>
    </Card>
  {/if}

  <div class="tiles">
    {#each tiles as t (t.key)}
      {@const tile = status.summary.tiles[t.key]}
      <a class="tile" href={tile.link} onclick={(e) => navigate(e, tile.link)}>
        <div class="tile-title">{t.title}</div>
        <Pill tone={tone(tile.level)}>{tile.label}</Pill>
        <div class="tile-detail subtle">
          {tile.detail ?? ''}{#if tile.at}{tile.detail ? ' ' : ''}{ago(tile.at)}{/if}
        </div>
      </a>
    {/each}
  </div>
{/if}

<style>
  .hero {
    background: var(--surface);
    border: 1px solid var(--border);
    border-left: 4px solid var(--border-strong);
    border-radius: var(--radius-card);
    padding: 1.4rem 1.4rem 1.2rem;
    display: grid;
    gap: 1.1rem;
    margin-bottom: 1rem;
  }
  .hero.ok,
  .hero.info {
    border-left-color: var(--green);
  }
  .hero.warn {
    border-left-color: var(--orange);
  }
  .hero.bad {
    border-left-color: var(--red);
  }
  .domain {
    font-size: 1.5rem;
    font-weight: 700;
    color: var(--text);
    overflow-wrap: anywhere;
  }
  .ip {
    color: var(--text-muted);
    margin-top: 0.2rem;
    overflow-wrap: anywhere;
  }
  .verdict {
    display: flex;
    gap: 0.75rem;
    align-items: flex-start;
  }
  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    margin-top: 0.4rem;
    flex: none;
    background: var(--text-subtle);
  }
  .ok .dot,
  .info .dot {
    background: var(--green);
  }
  .warn .dot {
    background: var(--orange);
  }
  .bad .dot {
    background: var(--red);
  }
  .headline {
    font-size: 1.15rem;
    font-weight: 600;
  }
  .line {
    color: var(--text-muted);
  }
  .buttons {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }
  .btn {
    border: 1px solid var(--border-strong);
    border-radius: 8px;
    padding: 0.45rem 0.9rem;
    color: var(--text);
  }
  .btn:hover {
    background: var(--surface-3);
    text-decoration: none;
  }
  .btn.primary {
    background: var(--primary);
    border-color: var(--primary);
    color: var(--text-on-accent);
  }
  .issues {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 0.6rem;
  }
  .issues li {
    display: flex;
    gap: 0.6rem;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .issues li span {
    flex: 1 1 16rem;
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 1rem;
    margin-top: 1rem;
  }
  @media (max-width: 860px) {
    .tiles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (max-width: 420px) {
    .tiles {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .tile {
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: var(--radius-card);
    padding: 0.9rem 1rem;
    display: grid;
    gap: 0.45rem;
    justify-items: start;
    color: var(--text);
  }
  .tile:hover {
    border-color: var(--border-strong);
    text-decoration: none;
  }
  .tile-title {
    font-weight: 600;
    font-size: 0.85rem;
  }
  .tile-detail {
    font-size: 0.8rem;
    min-height: 1em;
  }
  .small {
    font-size: 0.8rem;
  }
</style>
