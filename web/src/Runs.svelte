<script>
  import { get, send, why, onEvent, throttle, ago, runTime, duration, live, fmt, plotValue, store, loadProjects, progressText, stuck } from './lib.svelte.js'

  let { project } = $props()
  let runs = $state(null)
  let status = $state('')
  let picked = $state([])

  let limit = $state(200) // "Show more" raises it; the search asks the server, so it also finds runs past it
  let query = $state('')
  const load = throttle(() =>
    get(`/runs?project=${encodeURIComponent(project)}&status=${status}&metrics=1&limit=${limit}&q=${encodeURIComponent(query.trim())}`)
      .then((r) => (runs = r))
      .catch(() => {}),
  )
  const refresh = throttle(load, 10000) // running runs' numbers: often enough to follow, not once per logged step
  $effect(() => {
    status, limit, query
    load()
  })
  $effect(() =>
    onEvent((e) => {
      if (e.type === 'run' || e.type === 'reconnect') load()
      if (e.type === 'metrics' && runs?.some((r) => r.id === e.run)) refresh()
    }),
  )

  // Metric columns: the three keys logged by most runs, losses first.
  const cols = $derived.by(() => {
    const n = {}
    for (const r of runs || []) for (const k of Object.keys(r.metrics || {})) if (!k.startsWith('sys/')) n[k] = (n[k] || 0) + 1
    return Object.keys(n)
      .sort((a, b) => /loss/i.test(b) - /loss/i.test(a) || n[b] - n[a] || a.localeCompare(b))
      .slice(0, 3)
  })
  const filters = ['', 'running', 'finished', 'failed', 'crashed']

  // Search by name or tag, then sort by a clicked column; null keeps the server's order (newest first).
  let sort = $state(null) // {key, dir: 1 | -1}
  const value = {
    status: (r) => r.status,
    run: (r) => r.name,
    duration: (r) => (r.ended_at ? new Date(r.ended_at) : live.now) - new Date(r.started_at),
    started: (r) => -new Date(r.started_at), // ascending reads as the column does: fewest minutes ago first
  }
  const by = (key) => (sort = { key, dir: sort?.key === key ? -sort.dir : 1 })
  const aria = (key) => (sort?.key === key ? (sort.dir > 0 ? 'ascending' : 'descending') : null)
  const shown = $derived.by(() => {
    const q = query.trim().toLowerCase()
    const out = (runs || []).filter((r) => !q || r.name.toLowerCase().includes(q) || r.tags?.some((t) => t.toLowerCase().includes(q)))
    if (!sort) return out
    const v = value[sort.key] || ((r) => plotValue(r.metrics?.[sort.key.slice(2)]?.last)) // 'm:<metric>', so a metric named "status" can't clash
    const cmp = (a, b) => (typeof a === 'string' ? a.localeCompare(b, undefined, { numeric: true }) : a - b)
    // Missing values (no metric, NaN) go last in either direction.
    return out
      .map((r) => [v(r), r])
      .sort(([a], [b]) => (a == null) - (b == null) || (a == null ? 0 : cmp(a, b) * sort.dir))
      .map(([, r]) => r)
  })

  // The project's main metric: agent cards on the kiosk score by it. '' leaves it to a loss.
  const proj = $derived(store.projects?.find((p) => p.name === project))
  const keys = $derived([...new Set((runs || []).flatMap((r) => Object.keys(r.metrics || {}).filter((k) => !k.startsWith('sys/'))))].sort())
  let metricFailed = $state('')
  async function judge(metric, goal) {
    try {
      await send('PATCH', '/projects/' + encodeURIComponent(project), { metric, goal })
      metricFailed = ''
      loadProjects()
    } catch (err) {
      metricFailed = why(err)
    }
  }

  // Deleting asks for the project's name, typed: it takes every run with it.
  let confirming = $state(false)
  let typed = $state('')
  let failed = $state('')
  const total = $derived(store.projects?.find((p) => p.name === project)?.runs ?? 0)
  async function remove(e) {
    e.preventDefault()
    try {
      await send('DELETE', '/projects/' + encodeURIComponent(project))
      loadProjects()
      location.hash = '#/projects'
    } catch (err) {
      failed = why(err)
    }
  }
</script>

<p class="crumb"><a href="#/projects">Projects</a> /</p>
<div class="page-head">
  <h1>{project}</h1>
  <div class="tools">
    <input class="field" type="search" bind:value={query} placeholder="Search runs" aria-label="Search runs by name or tag" />
    <div class="filters" role="group" aria-label="Show runs by status">
      {#each filters as f}
        <button aria-pressed={status === f} onclick={() => (status = f)}>{f || 'All'}</button>
      {/each}
    </div>
  </div>
</div>

{#if keys.length}
  <p class="judge muted small">
    <label for="judge-metric">Judged by</label>
    <select id="judge-metric" class="field" value={proj?.metric || ''} onchange={(e) => judge(e.target.value, proj?.goal || 'min')}>
      <option value="">a loss</option>
      {#each keys as k}<option value={k}>{k}</option>{/each}
    </select>
    <select class="field" value={proj?.goal || 'min'} disabled={!proj?.metric} onchange={(e) => judge(proj.metric, e.target.value)} aria-label="Which way is better">
      <option value="min">lower is better</option>
      <option value="max">higher is better</option>
    </select>
    {#if metricFailed}<span><span class="danger">Failed.</span> {metricFailed}</span>{/if}
  </p>
{/if}

{#snippet head(key, label, cls = '')}
  <th class={cls} aria-sort={aria(key)}>
    <button class="sort" onclick={() => by(key)}>{label}{#if sort?.key === key}<span aria-hidden="true">{sort.dir > 0 ? ' ↑' : ' ↓'}</span>{/if}</button>
  </th>
{/snippet}

{#if runs?.length === 0}
  <p class="empty">No {status} runs in this project.</p>
{:else if runs && !shown.length}
  <p class="empty">No runs match “{query.trim()}”.</p>
{:else if runs}
  <div class="card table">
    <table>
      <thead>
        <tr>
          <th class="pick"><span class="sr">Compare</span></th>
          {@render head('status', 'Status', 'st')}
          {@render head('run', 'Run', 'name')}
          {@render head('duration', 'Duration')}
          <th>Progress</th>
          {@render head('started', 'Started')}
          <th>Tags</th>
          {#each cols as c}{@render head('m:' + c, c, 'num')}{/each}
        </tr>
      </thead>
      <tbody>
        {#each shown as r (r.id)}
          {@const m = r.metrics}
          <tr class="status-{r.status}">
            <td class="pick"><input type="checkbox" bind:group={picked} value={r.id} aria-label="Compare {r.name}" /></td>
            <td class="st"><span class="badge">{r.status}</span>{#if stuck(r, live.now)}<span class="small quiet" title="No metric and no output line since then">no output {duration(stuck(r, live.now))}</span>{/if}</td>
            <td class="name"><a href="#/r/{r.id}">{r.name}</a>{#if r.host}<span class="muted small host">{r.host}</span>{/if}</td>
            <td>{runTime(r, live.now)}</td>
            <td class="muted">{(r.status !== 'finished' && progressText(r, live.now).split(' · ').slice(1).join(' · ')) || '–'}</td>
            <td class="muted" title={new Date(r.started_at).toLocaleString()}>{ago(r.started_at, live.now)}</td>
            <td>{#each r.tags as t}<span class="tag">{t}</span>{/each}</td>
            {#each cols as c}<td class="num">{#if m?.[c]}{fmt(m[c].last)}{:else}<span class="muted">–</span>{/if}</td>{/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
  {#if runs.length >= limit && limit < 1000}
    <p class="more muted small">The newest {runs.length} of {total} runs. <button onclick={() => (limit += 200)}>Show more</button> Search looks through all of them.</p>
  {:else if runs.length >= 1000}
    <p class="more muted small">The newest 1,000 of {total} runs. Search looks through all of them.</p>
  {/if}
{/if}

<div class="remove">
  {#if confirming}
    <form class="card card-pad" onsubmit={remove}>
      <p class="small">This deletes {project} and its {total === 1 ? '1 run' : `${total} runs`} with their metrics, logs, samples and artifacts, for good. Type the project's name to confirm.</p>
      <div class="confirm">
        <input class="field" bind:value={typed} aria-label="Project name" autocomplete="off" autocapitalize="none" spellcheck="false" />
        <button class="danger" disabled={typed !== project}>Delete project</button>
        <button type="button" onclick={() => ((confirming = false), (typed = failed = ''))}>Cancel</button>
      </div>
    </form>
  {:else}
    <button class="danger" onclick={() => (confirming = true)}>Delete project</button>
  {/if}
  {#if failed}<p class="small"><span class="danger">Failed.</span> {failed}</p>{/if}
</div>

{#if picked.length >= 2}
  <div class="compare">
    <a class="go" href="#/compare/{picked.join(',')}">Compare {picked.length} runs</a>
    <button onclick={() => (picked = [])}>Clear</button>
  </div>
{/if}

<style>
  .more {
    margin: 10px 0 0;
  }
  .more button {
    margin: 0 6px;
  }
  .quiet {
    display: block;
    color: var(--crashed);
  }
  .judge {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: -8px 0 16px;
  }
  .judge .field {
    height: 32px;
  }
  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .tools input {
    flex: 1 1 200px;
  }
  .filters {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  /* The compare box, status and name stay put while wide metric columns scroll under them. */
  .pick,
  .st,
  .name {
    position: sticky;
    z-index: 1;
    background: var(--panel);
  }
  tr:hover > td:is(.pick, .st, .name) {
    background: var(--soft);
  }
  .pick {
    left: 0;
    width: 30px;
    min-width: 30px;
    padding-right: 0;
  }
  .st {
    left: 30px;
    width: 108px;
    min-width: 108px;
  }
  .name {
    left: 138px;
  }
  /* A phone has no room for three pinned columns: only the name stays, shortened. */
  @media (max-width: 767px) {
    .pick,
    .st {
      position: static;
    }
    .name {
      left: 0;
    }
    .name > * {
      display: block;
      max-width: 40vw;
      overflow: hidden;
      text-overflow: ellipsis;
    }
  }
  .sort {
    padding: 0;
    border: 0;
    background: none;
    font: inherit;
    color: inherit;
  }
  .sort:hover {
    background: none;
    color: var(--ink);
  }
  .pick input {
    display: block;
    width: 16px;
    height: 16px;
    margin: 0;
    accent-color: var(--ink);
  }
  td.name {
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
  .remove {
    margin-top: 24px;
  }
  .remove p {
    margin: 0 0 10px;
  }
  .confirm {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .confirm input {
    flex: 1 1 220px;
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
    border-radius: var(--radius-sm);
    padding: 8px 16px;
    text-decoration: none;
    font-weight: 600;
    font-size: 0.875rem;
  }
</style>
