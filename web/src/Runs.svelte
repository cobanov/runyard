<script>
  import { get, onEvent, throttle, ago, runTime, live, fmt } from './lib.svelte.js'

  let { project } = $props()
  let runs = $state(null)
  let status = $state('')
  let picked = $state([])

  const load = throttle(() => get(`/runs?project=${encodeURIComponent(project)}&status=${status}&metrics=1`).then((r) => (runs = r)))
  $effect(() => {
    status
    load()
  })
  $effect(() => onEvent((e) => (e.type === 'run' || e.type === 'reconnect') && load()))

  // Metric columns: the three keys logged by most runs, losses first.
  const cols = $derived.by(() => {
    const n = {}
    for (const r of runs || []) for (const k of Object.keys(r.metrics || {})) if (!k.startsWith('sys/')) n[k] = (n[k] || 0) + 1
    return Object.keys(n)
      .sort((a, b) => /loss/i.test(b) - /loss/i.test(a) || n[b] - n[a] || a.localeCompare(b))
      .slice(0, 3)
  })
  const filters = ['', 'running', 'finished', 'failed', 'crashed']
</script>

<p class="crumb"><a href="#/projects">Projects</a> /</p>
<div class="page-head">
  <h1>{project}</h1>
  <div class="filters" role="group" aria-label="Show runs by status">
    {#each filters as f}
      <button aria-pressed={status === f} onclick={() => (status = f)}>{f || 'All'}</button>
    {/each}
  </div>
</div>

{#if runs?.length === 0}
  <p class="empty">No {status} runs in this project.</p>
{:else if runs}
  <div class="card table">
    <table>
      <thead>
        <tr>
          <th class="pick"><span class="sr">Compare</span></th>
          <th>Status</th>
          <th>Run</th>
          <th>Duration</th>
          <th>Started</th>
          <th>Tags</th>
          {#each cols as c}<th class="num">{c}</th>{/each}
        </tr>
      </thead>
      <tbody>
        {#each runs as r (r.id)}
          {@const m = r.metrics}
          <tr class="status-{r.status}">
            <td class="pick"><input type="checkbox" bind:group={picked} value={r.id} aria-label="Compare {r.name}" /></td>
            <td><span class="badge">{r.status}</span></td>
            <td class="name"><a href="#/r/{r.id}">{r.name}</a>{#if r.host}<span class="muted small host">{r.host}</span>{/if}</td>
            <td>{runTime(r, live.now)}</td>
            <td class="muted">{ago(r.started_at, live.now)}</td>
            <td>{#each r.tags as t}<span class="tag">{t}</span>{/each}</td>
            {#each cols as c}<td class="num">{m?.[c] ? fmt(m[c].last) : ''}</td>{/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

{#if picked.length >= 2}
  <div class="compare">
    <a class="go" href="#/compare/{picked.join(',')}">Compare {picked.length} runs</a>
    <button onclick={() => (picked = [])}>Clear</button>
  </div>
{/if}

<style>
  .filters {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .pick {
    width: 1px;
    padding-right: 0;
  }
  input {
    display: block;
    width: 16px;
    height: 16px;
    margin: 0;
    accent-color: var(--ink);
  }
  .name {
    font-weight: 600;
  }
  .host {
    display: block;
    font-weight: 400;
  }
  .name a {
    text-decoration: none;
  }
  .name a:hover {
    text-decoration: underline;
  }
  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  .tag + .tag {
    margin-left: 4px;
  }
  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
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
    border-radius: 8px;
    padding: 8px 16px;
    text-decoration: none;
    font-weight: 600;
    font-size: 0.875rem;
  }
</style>
