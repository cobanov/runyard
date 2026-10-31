<script>
  import { live, onEvent } from './lib.svelte.js'
  import Projects from './Projects.svelte'
  import Runs from './Runs.svelte'
  import RunPage from './RunPage.svelte'
  import Compare from './Compare.svelte'
  import Login from './Login.svelte'
  import { pushSupported, pushOn, setPush } from './push.js'

  // Hash routes: #/ projects, #/p/<project> runs, #/r/<run id>, #/compare/<id>,<id>...
  let hash = $state(location.hash)
  window.addEventListener('hashchange', () => (hash = location.hash))
  const route = $derived.by(() => {
    const mlflow = hash.match(/^#\/experiments\/[^/]+\/runs\/([^/]+)/) // the run links mlflow's client prints
    if (mlflow) return { kind: 'r', arg: mlflow[1] }
    const [, kind, arg] = hash.match(/^#\/(\w+)\/(.+)$/) || []
    return { kind, arg: arg && decodeURIComponent(arg) }
  })

  // me: {auth, user}; with RUNYARD_AUTH on and no user yet, the login form shows instead of the app
  let me = $state(null)
  fetch('/api/v1/me')
    .then((r) => r.json())
    .then((m) => (me = m))
  const ready = $derived(me && (!me.auth || me.user))
  $effect(() => {
    if (ready) onEvent(() => {}) // open the stream now, so the connection dot is right from the first paint
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

<header class="wrap">
  <a href="#/" class="brand">runyard</a>
  <span class="dot" class:on={live.connected} title={live.connected ? 'Live: updates arrive as they happen' : 'Reconnecting to the server'}></span>
  {#if me?.auth && me.user}
    <button class="user" onclick={logout} title="Log out">{me.user}</button>
  {/if}
  {#if pushSupported && ready}
    <button class="bell" class:on={push} onclick={togglePush} aria-pressed={push} title={pushError || (push ? 'Notifications on: tap to turn off' : 'Notify this device when runs finish, fail or alert')}>
      <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true"><path d="M6 16V11a6 6 0 0 1 12 0v5l2 2H4zM10 20a2 2 0 0 0 4 0" fill={push ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.8" stroke-linejoin="round" /></svg>
      <span class="sr">{push ? 'Notifications on' : 'Turn on notifications'}</span>
    </button>
  {/if}
</header>

<main class="wrap">
  {#if me && !ready}
    <Login onlogin={(user) => (me = { ...me, user })} />
  {:else if ready}
  {#key hash}
    {#if route.kind === 'p'}
      <Runs project={route.arg} />
    {:else if route.kind === 'r'}
      <RunPage id={route.arg} />
    {:else if route.kind === 'compare'}
      <Compare ids={route.arg.split(',')} />
    {:else}
      <Projects />
    {/if}
  {/key}
  {/if}
</main>

<style>
  header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding-top: 14px;
    padding-bottom: 14px;
  }
  .brand {
    font-weight: 800;
    font-size: 1.25rem;
    letter-spacing: -0.02em;
    text-decoration: none;
  }
  .dot {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--crashed);
  }
  .dot.on {
    background: var(--running);
  }
  .user {
    margin-left: auto;
    border: 0;
    background: none;
    color: var(--muted);
    font-size: 0.875rem;
  }
  .user + .bell {
    margin-left: 0;
  }
  .bell {
    margin-left: auto;
    display: grid;
    place-items: center;
    width: 40px;
    height: 40px;
    border: 0;
    border-radius: 50%;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }
  .bell.on {
    color: var(--running);
  }
  .bell:focus-visible {
    outline: 2px solid var(--running);
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }
</style>
