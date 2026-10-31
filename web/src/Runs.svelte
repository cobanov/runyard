<script>
  import { get, onEvent, throttle, ago, runTime, live } from './lib.svelte.js'

  let { project } = $props()
  let runs = $state(null)
  let status = $state('')
  let picked = $state([])

  const load = throttle(() =>
    get(`/runs?project=${encodeURIComponent(project)}&status=${status}`).then((r) => (runs = r)),
  )
  $effect(() => {
    status
    load()
  })
  $effect(() => onEvent((e) => (e.type === 'run' || e.type === 'reconnect') && load()))

  const filters = ['', 'running', 'finished', 'failed', 'crashed']
</script>

<p class="muted small crumb"><a href="#/">Projects</a> /</p>
<h1>{project}</h1>

<div class="filters" role="group" aria-label="Show runs by status">
  {#each filters as f}
    <button aria-pressed={status === f} onclick={() => (status = f)}>{f || 'all'}</button>
  {/each}
</div>

{#if runs?.length === 0}
  <p class="empty">No {status} runs in this project.</p>
{:else if runs}
  <ul class="row-list">
    {#each runs as r (r.id)}
      <li class="status-{r.status}">
        <div class="edge row">
          <input type="checkbox" bind:group={picked} value={r.id} aria-label="Compare {r.name}" />
          <a href="#/r/{r.id}">
            <span class="name">{r.name}</span>
            <span class="small">
              <span class="state">{r.status}</span>
              <span class="muted">for {runTime(r, live.now)}, started {ago(r.started_at, live.now)}{r.host ? ` on ${r.host}` : ''}</span>
            </span>
            {#if r.tags.length}<span class="tags small">{#each r.tags as t}<span>{t}</span>{/each}</span>{/if}
          </a>
        </div>
      </li>
    {/each}
  </ul>
{/if}

{#if picked.length >= 2}
  <div class="compare">
    <a class="go" href="#/compare/{picked.join(',')}">Compare {picked.length} runs</a>
    <button onclick={() => (picked = [])}>Clear</button>
  </div>
{/if}

<style>
  .crumb {
    margin: 0;
  }
  h1 {
    margin-bottom: 12px;
  }
  .filters {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    margin-bottom: 14px;
  }
  .row {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 12px;
    border-bottom: 1px solid var(--rule);
  }
  .row:hover {
    background: var(--panel);
  }
  input {
    margin-top: 5px;
    width: 18px;
    height: 18px;
    accent-color: var(--ink);
  }
  .row a {
    display: flex;
    flex-direction: column;
    gap: 2px;
    text-decoration: none;
    flex: 1;
    min-width: 0;
  }
  .name {
    font-weight: 700;
    overflow-wrap: anywhere;
  }
  .tags {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .tags span {
    border: 1px solid var(--rule);
    border-radius: 999px;
    padding: 0 8px;
  }
  .compare {
    position: sticky;
    bottom: 12px;
    display: flex;
    gap: 8px;
    justify-content: center;
    margin-top: 16px;
  }
  .go {
    background: var(--ink);
    color: var(--paper);
    border-radius: 6px;
    padding: 8px 16px;
    text-decoration: none;
    font-weight: 700;
  }
</style>
