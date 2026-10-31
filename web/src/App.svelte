<script>
  import { live, onEvent } from './lib.svelte.js'
  import Projects from './Projects.svelte'
  import Runs from './Runs.svelte'
  import RunPage from './RunPage.svelte'
  import Compare from './Compare.svelte'

  // Hash routes: #/ projects, #/p/<project> runs, #/r/<run id>, #/compare/<id>,<id>...
  let hash = $state(location.hash)
  window.addEventListener('hashchange', () => (hash = location.hash))
  const route = $derived.by(() => {
    const [, kind, arg] = hash.match(/^#\/(\w+)\/(.+)$/) || []
    return { kind, arg: arg && decodeURIComponent(arg) }
  })

  onEvent(() => {}) // open the stream now, so the connection dot is right from the first paint
</script>

<header class="wrap">
  <a href="#/" class="brand">runyard</a>
  <span class="dot" class:on={live.connected} title={live.connected ? 'Live: updates arrive as they happen' : 'Reconnecting to the server'}></span>
</header>

<main class="wrap">
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
</style>
