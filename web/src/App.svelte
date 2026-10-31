<script>
  import { live, onEvent, store, loadProjects } from './lib.svelte.js'
  import Overview from './Overview.svelte'
  import Projects from './Projects.svelte'
  import Runs from './Runs.svelte'
  import RunPage from './RunPage.svelte'
  import Compare from './Compare.svelte'
  import Alerts from './Alerts.svelte'
  import Login from './Login.svelte'
  import Settings from './Settings.svelte'
  import { pushSupported, pushOn, setPush } from './push.js'

  // Hash routes: #/ overview, #/projects, #/alerts, #/settings, #/p/<project> runs, #/r/<run id>, #/compare/<id>,<id>...
  let hash = $state(location.hash)
  let open = $state(false) // the drawer on narrow screens
  window.addEventListener('hashchange', () => ((hash = location.hash), (open = false)))
  const route = $derived.by(() => {
    const mlflow = hash.match(/^#\/experiments\/[^/]+\/runs\/([^/]+)/) // the run links mlflow's client prints
    if (mlflow) return { kind: 'r', arg: mlflow[1] }
    const [, kind, arg] = hash.match(/^#\/(\w+)\/?(.*)$/) || []
    return { kind, arg: arg && decodeURIComponent(arg) }
  })
  const project = $derived(route.kind === 'p' ? route.arg : null)

  // me: {auth, user}; with RUNYARD_AUTH on and no user yet, the login form shows instead of the app
  let me = $state(null)
  fetch('/api/v1/me')
    .then((r) => r.json())
    .then((m) => (me = m))
  const ready = $derived(me && (!me.auth || me.user))
  $effect(() => {
    if (!ready) return
    loadProjects()
    return onEvent((e) => (e.type === 'run' || e.type === 'reconnect') && loadProjects())
  })
  async function logout() {
    await fetch('/api/v1/logout', { method: 'POST' })
    location.reload() // drops the event stream and every page's data
  }

  let push = $state(false)
  let pushError = $state('')
  if (pushSupported) pushOn().then((on) => (push = on))
  async function togglePush() {
    pushError = ''
    try {
      push = await setPush(!push)
    } catch (e) {
      pushError = e.message
    }
  }
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && (open = false)} />

{#if me && !ready}
  <main class="login"><Login onlogin={(user) => (me = { ...me, user })} /></main>
{:else if ready}
  <div class="shell">
    <header class="top">
      <button class="icon" onclick={() => (open = !open)} aria-expanded={open} aria-controls="nav" aria-label="Menu">
        <svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
      </button>
      <a href="#/" class="brand">runyard</a>
      <span class="live" class:on={live.connected} title={live.connected ? 'Live' : 'Reconnecting'}></span>
    </header>
    {#if open}<div class="scrim" onclick={() => (open = false)} aria-hidden="true"></div>{/if}

    <nav id="nav" class="side" class:open aria-label="Main">
      <a href="#/" class="brand">
        runyard
        <span class="live" class:on={live.connected} title={live.connected ? 'Live: updates arrive as they happen' : 'Reconnecting to the server'}></span>
      </a>
      <a href="#/" class="item" aria-current={!route.kind || null}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 13h6V4H4zM14 20h6v-9h-6zM4 20h6v-4H4zM14 7h6V4h-6z" /></svg>Overview
      </a>
      <a href="#/projects" class="item" aria-current={route.kind === 'projects' || null}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" /></svg>Projects
      </a>
      {#if store.projects?.length}
        <ul class="projects">
          {#each store.projects as p (p.name)}
            <li>
              <a href="#/p/{encodeURIComponent(p.name)}" class="item sub" aria-current={project === p.name || null}>
                <span class="name">{p.name}</span>
                {#if p.running}<span class="pip status-running" title="{p.running} running"></span>{/if}
              </a>
            </li>
          {/each}
        </ul>
      {/if}
      <a href="#/alerts" class="item" aria-current={route.kind === 'alerts' || null}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3 2 20h20zM12 10v4M12 17v.5" /></svg>Alerts
      </a>
      <a href="#/settings" class="item" aria-current={route.kind === 'settings' || null}>
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6" /></svg>Settings
      </a>

      <div class="foot">
        {#if pushSupported}
          <button class="item bell" class:on={push} onclick={togglePush} aria-pressed={push} title={pushError || (push ? 'Notifications on: tap to turn off' : 'Notify this device when runs finish, fail or alert')}>
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4zM10 20a2 2 0 0 0 4 0" fill={push ? 'currentColor' : 'none'} /></svg>
            {push ? 'Notifications on' : 'Notifications'}
          </button>
        {/if}
        {#if me.auth && me.user}
          <button class="item" onclick={logout} title="Log out">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10 4H5v16h5M14 8l4 4-4 4M18 12H9" /></svg>
            <span class="name">{me.user}</span><span class="muted small">Log out</span>
          </button>
        {/if}
      </div>
    </nav>

    <main>
      {#key hash}
        {#if route.kind === 'p'}
          <Runs project={route.arg} />
        {:else if route.kind === 'r'}
          <RunPage id={route.arg} />
        {:else if route.kind === 'compare'}
          <Compare ids={route.arg.split(',')} />
        {:else if route.kind === 'projects'}
          <Projects />
        {:else if route.kind === 'alerts'}
          <Alerts />
        {:else if route.kind === 'settings'}
          <Settings auth={me.auth} />
        {:else}
          <Overview />
        {/if}
      {/key}
    </main>
  </div>
{/if}

<style>
  .shell {
    display: grid;
    grid-template-columns: 232px minmax(0, 1fr);
    min-height: 100dvh;
  }
  main {
    padding: 28px 32px 56px;
    max-width: 1280px;
    width: 100%;
  }
  .login {
    display: grid;
    place-items: center;
    min-height: 100dvh;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 800;
    font-size: 1.125rem;
    letter-spacing: -0.02em;
    text-decoration: none;
    padding: 6px 10px;
    margin-bottom: 12px;
  }
  .live {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--crashed);
  }
  .live.on {
    background: var(--running);
  }
  .side {
    position: sticky;
    top: 0;
    height: 100dvh;
    display: flex;
    flex-direction: column;
    padding: 16px 12px;
    border-right: 1px solid var(--rule);
    background: var(--paper);
    overflow-y: auto;
  }
  .item {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 6px 10px;
    border: 0;
    border-radius: var(--radius-sm);
    background: none;
    color: var(--ink);
    font-size: 0.875rem;
    font-weight: 400;
    text-decoration: none;
    text-align: left;
    cursor: pointer;
  }
  .item:hover {
    background: var(--soft);
  }
  .item[aria-current] {
    background: var(--soft);
    font-weight: 500;
  }
  .item svg {
    width: 18px;
    height: 18px;
    flex: none;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linejoin: round;
    stroke-linecap: round;
    color: var(--muted);
  }
  .item[aria-current] svg {
    color: var(--ink);
  }
  .projects {
    list-style: none;
    margin: 2px 0 2px 18px;
    padding: 0 0 0 10px;
    border-left: 1px solid var(--rule);
  }
  .sub {
    padding: 5px 10px;
    font-size: 0.875rem;
    color: var(--muted);
  }
  .sub[aria-current] {
    color: var(--ink);
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .foot {
    margin-top: auto;
    padding-top: 12px;
    border-top: 1px solid var(--rule);
  }
  .bell.on,
  .bell.on svg {
    color: var(--running);
  }
  .top,
  .scrim {
    display: none;
  }
  .icon {
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    padding: 0;
    border: 0;
    background: none;
    color: var(--ink);
  }
  @media (max-width: 767px) {
    .shell {
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto 1fr; /* the top bar keeps its height; main takes the rest */
    }
    main {
      padding: 16px 16px 48px;
    }
    .top {
      display: flex;
      align-items: center;
      gap: 4px;
      position: sticky;
      top: 0;
      z-index: 2;
      padding: 6px 8px;
      background: var(--paper);
      border-bottom: 1px solid var(--rule);
    }
    .top .brand {
      margin: 0;
    }
    .side {
      position: fixed;
      inset: 0 auto 0 0;
      width: min(280px, 85vw);
      z-index: 4;
      transform: translateX(-100%);
      transition: transform 0.2s ease;
      box-shadow: 0 0 0 1px var(--rule);
    }
    .side.open {
      transform: none;
    }
    .scrim {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 3;
      background: rgb(0 0 0 / 0.4);
    }
    @media (prefers-reduced-motion: reduce) {
      .side {
        transition: none;
      }
    }
  }
</style>
