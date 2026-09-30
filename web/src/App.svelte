<script lang="ts">
  import Overview from './pages/Overview.svelte'
  import Update from './pages/Update.svelte'
  import Domain from './pages/Domain.svelte'
  import Email from './pages/Email.svelte'
  import Diagnostics from './pages/Diagnostics.svelte'

  const pages = [
    { path: '/', label: 'Overview', component: Overview },
    { path: '/email', label: 'Email', component: Email },
    { path: '/update', label: 'Update', component: Update },
    { path: '/domain', label: 'Domain', component: Domain },
    { path: '/diagnostics', label: 'Diagnostics', component: Diagnostics },
  ]

  let path = $state(location.pathname)
  const current = $derived(pages.find((p) => p.path === path) ?? pages[0])

  function go(e: MouseEvent, to: string) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return
    e.preventDefault()
    if (to !== path) history.pushState(null, '', to)
    path = to
  }

  $effect(() => {
    const onPop = () => (path = location.pathname)
    addEventListener('popstate', onPop)
    return () => removeEventListener('popstate', onPop)
  })

  $effect(() => {
    document.title = current.path === '/' ? 'Mesh Console' : `${current.label} · Mesh Console`
  })
</script>

<header class="top">
  <div class="inner">
    <a class="brand" href="/" onclick={(e) => go(e, '/')}>
      <img src="/icon.png" alt="" width="24" height="24" />
      <span>Mesh Console</span>
    </a>
    <nav>
      {#each pages as p (p.path)}
        <a href={p.path} class:active={p.path === current.path} onclick={(e) => go(e, p.path)}>{p.label}</a>
      {/each}
    </nav>
  </div>
</header>

<main>
  {#key current.path}
    <current.component />
  {/key}
</main>

<style>
  .top {
    background: var(--surface);
    border-bottom: 1px solid var(--border);
    position: sticky;
    top: 0;
    z-index: 10;
  }
  .inner {
    max-width: 1100px;
    margin: 0 auto;
    padding: 0 16px;
    display: flex;
    align-items: center;
    gap: 1.5rem;
    height: 52px;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    font-weight: 600;
    color: var(--text);
    white-space: nowrap;
  }
  .brand:hover {
    text-decoration: none;
  }
  nav {
    display: flex;
    gap: 0.25rem;
    overflow-x: auto;
  }
  nav a {
    padding: 0.4rem 0.7rem;
    border-radius: 8px;
    color: var(--text-muted);
    white-space: nowrap;
  }
  nav a:hover {
    background: var(--surface-2);
    text-decoration: none;
  }
  nav a.active {
    background: var(--surface-3);
    color: var(--text);
    font-weight: 600;
  }
  main {
    max-width: 1100px;
    margin: 0 auto;
    padding: 1.25rem 16px 3rem;
  }
  @media (max-width: 520px) {
    .brand span {
      display: none;
    }
  }
</style>
