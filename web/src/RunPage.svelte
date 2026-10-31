<script>
  import { get, send, why, onEvent, throttle, runTime, ago, live, plotValue, fmt, flatten } from './lib.svelte.js'
  import Chart from './Chart.svelte'
  import Samples from './Samples.svelte'
  import Logs from './Logs.svelte'

  let { id } = $props()
  let run = $state(null)
  let error = $state('')
  let series = $state({})
  let tab = $state('charts')
  let alerts = $state([])
  let artifacts = $state([])

  const loadRun = throttle(() => get('/runs/' + id).then((r) => (run = r)).catch((e) => (error = e.message)))
  const loadMetrics = throttle(() => get(`/runs/${id}/metrics?points=600`).then((s) => (series = s)), 4000)
  const loadAlerts = throttle(() => get(`/alerts?run=${id}&limit=10`).then((a) => (alerts = a)))
  const loadArtifacts = throttle(() => get(`/runs/${id}/artifacts`).then((a) => (artifacts = a)))
  loadRun()
  loadMetrics()
  loadAlerts()
  loadArtifacts()
  $effect(() =>
    onEvent((e) => {
      if (e.type === 'reconnect') (loadRun(), loadMetrics(), loadAlerts())
      if (e.run !== id) return
      if (e.type === 'run') loadRun()
      if (e.type === 'metrics') loadMetrics()
      if (e.type === 'alert') loadAlerts()
      if (e.type === 'artifact') loadArtifacts()
    }),
  )

  const keys = $derived(Object.keys(series).sort())
  const trainKeys = $derived(keys.filter((k) => !k.startsWith('sys/')))
  const sysKeys = $derived(keys.filter((k) => k.startsWith('sys/')))
  const tabs = $derived([
    ['charts', 'Charts'],
    ['samples', 'Samples'],
    ['logs', 'Logs'],
    ['config', 'Config'],
    ...(artifacts.length ? [['artifacts', 'Artifacts']] : []),
    ...(sysKeys.length ? [['system', 'System']] : []),
  ])
  const lineOf = (k) => {
    const s = series[k]
    return [{ label: k, color: '--c1', x: s.step, y: s.last.map(plotValue), min: s.min.map(plotValue), max: s.max.map(plotValue) }]
  }
  const bytes = (n) => (n < 1e3 ? `${n} B` : n < 1e6 ? `${(n / 1e3).toFixed(1)} kB` : n < 1e9 ? `${(n / 1e6).toFixed(1)} MB` : `${(n / 1e9).toFixed(2)} GB`)
  let failed = $state('')
  async function remove() {
    const still = run.status === 'running' ? ' It is still running; what it sends from now on is refused.' : ''
    if (!confirm(`Delete ${run.name} with its metrics, logs, samples and artifacts? This cannot be undone.${still}`)) return
    try {
      await send('DELETE', '/runs/' + run.id)
      location.hash = '#/p/' + encodeURIComponent(run.project)
    } catch (e) {
      failed = why(e)
    }
  }
  let editing = $state(null) // {tags, notes} while they are being edited
  const edit = () => (editing = { tags: (run.tags || []).join(', '), notes: run.notes || '' })
  async function save(e) {
    e.preventDefault()
    failed = ''
    try {
      await send('PATCH', `/runs/${run.id}/meta`, { notes: editing.notes, tags: editing.tags.split(',') })
      editing = null
      loadRun()
    } catch (err) {
      failed = why(err)
    }
  }
  const config = $derived(Object.entries(flatten(run?.config)))
  const summary = $derived(Object.entries(flatten(run?.summary)))
</script>

{#if error}
  <p class="empty">Could not load this run: {error}</p>
{:else if run}
  <p class="crumb"><a href="#/projects">Projects</a> / <a href="#/p/{encodeURIComponent(run.project)}">{run.project}</a> /</p>
  <div class="card card-pad head status-{run.status}">
    <div class="title">
      <h1>{run.name}</h1>
      <span class="badge">{run.status}</span>
      <button class="danger delete" onclick={remove}>Delete run</button>
    </div>
    {#if failed}<p class="small fail"><span class="danger">Failed.</span> {failed}</p>{/if}
    <dl class="facts small">
      <div><dt>Duration</dt><dd>{runTime(run, live.now)}</dd></div>
      <div><dt>Started</dt><dd>{ago(run.started_at, live.now)}</dd></div>
      {#if run.status === 'running'}<div><dt>Heartbeat</dt><dd>{ago(run.last_heartbeat_at, live.now)}</dd></div>{/if}
      {#if run.host}<div><dt>Host</dt><dd><a href="#/machines">{run.host}</a></dd></div>{/if}
      {#if run.git?.commit}<div><dt>Git</dt><dd>{run.git.branch} {run.git.commit.slice(0, 8)}{run.git.dirty ? ', uncommitted changes' : ''}</dd></div>{/if}
    </dl>
    {#if editing}
      <form class="edit" onsubmit={save}>
        <label class="small">Tags, separated by commas <input class="field" bind:value={editing.tags} placeholder="baseline, lr-sweep" /></label>
        <label class="small">Notes <textarea class="field" rows="4" bind:value={editing.notes} placeholder="What this run tries, what you saw"></textarea></label>
        <div class="actions"><button class="btn-primary">Save</button><button type="button" onclick={() => (editing = null)}>Cancel</button></div>
      </form>
    {:else}
      {#if run.tags?.length}<p class="tags">{#each run.tags as t}<span class="tag">{t}</span>{/each}</p>{/if}
      {#if run.notes}<p class="small notes">{run.notes}</p>{/if}
      <button class="link small" onclick={edit}>{run.tags?.length || run.notes ? 'Edit notes and tags' : 'Add notes or tags'}</button>
    {/if}
  </div>

  {#if alerts.length}
    <ul class="card row-list alerts small" aria-label="Alerts">
      {#each alerts as a (a.id)}
        <li class="level-{a.level}">
          <strong>{a.title}</strong>
          <span class="muted">{a.body ? a.body.split('\n')[0] + ', ' : ''}{ago(a.ts, live.now)}</span>
        </li>
      {/each}
    </ul>
  {/if}

  <div class="tabs" role="tablist">
    {#each tabs as [t, label]}
      <button role="tab" aria-selected={tab === t} onclick={() => (tab = t)}>{label}</button>
    {/each}
  </div>

  {#if tab === 'charts'}
    {#if trainKeys.length}
      <div class="grid">
        {#each trainKeys as k (k)}
          <Chart title={k} lines={lineOf(k)} />
        {/each}
      </div>
    {:else}
      <p class="empty">No metrics yet. They appear here as soon as the script calls <code>run.log()</code>.</p>
    {/if}
  {:else if tab === 'system'}
    <div class="grid">
      {#each sysKeys as k (k)}
        <Chart title={k.slice(4)} lines={lineOf(k)} xLabel="second" />
      {/each}
    </div>
  {:else if tab === 'samples'}
    <Samples {id} />
  {:else if tab === 'logs'}
    <Logs {id} running={run.status === 'running'} />
  {:else if tab === 'artifacts'}
    <ul class="card row-list artifacts">
      {#each artifacts as a (a.id)}
        <li>
          <div class="edge-row">
            {#if a.url}<a href={a.url} download>{a.name}</a>{:else}<span>{a.name}</span>{/if}
            <span class="muted small">{a.kind}{a.step != null ? `, step ${fmt(a.step)}` : ''}{a.size != null ? `, ${bytes(a.size)}` : ''}</span>
          </div>
          {#if !a.url && a.uri}<code class="small muted">{a.uri}</code>{/if}
        </li>
      {/each}
    </ul>
  {:else if tab === 'config'}
    {#if summary.length}
      <h2>Summary</h2>
      <dl class="card kv">{#each summary as [k, v]}<dt>{k}</dt><dd>{fmt(v)}</dd>{/each}</dl>
    {/if}
    <h2>Config</h2>
    {#if config.length}
      <dl class="card kv">{#each config as [k, v]}<dt>{k}</dt><dd>{fmt(v)}</dd>{/each}</dl>
    {:else}
      <p class="empty">This run was started without a config.</p>
    {/if}
  {/if}
{/if}

<style>
  .title {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 14px;
  }
  .delete {
    margin-left: auto;
  }
  .fail {
    margin: 8px 0 0;
  }
  .facts {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 24px;
    margin: 12px 0 0;
  }
  .facts dt {
    color: var(--muted);
  }
  .facts dd {
    margin: 0;
    font-weight: 500;
    overflow-wrap: anywhere;
  }
  .tags {
    margin: 10px 0 0;
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
  }
  .notes {
    margin: 10px 0 0;
    white-space: pre-wrap;
  }
  .link {
    display: block;
    margin-top: 10px;
    padding: 0;
    border: 0;
    background: none;
    color: var(--muted);
    font-weight: 400;
    text-decoration: underline;
  }
  .edit {
    display: grid;
    gap: 10px;
    margin-top: 12px;
  }
  .edit label {
    display: grid;
    gap: 4px;
    color: var(--muted);
  }
  .edit .field {
    color: var(--ink);
  }
  .edit textarea {
    height: auto;
    padding: 8px 12px;
    resize: vertical;
  }
  .actions {
    display: flex;
    gap: 8px;
  }
  .alerts {
    margin: 14px 0 0;
  }
  .alerts li {
    border-left: 3px solid var(--muted);
    padding: 8px 14px;
    overflow-wrap: anywhere;
  }
  .alerts li:first-child {
    border-top-left-radius: var(--radius);
  }
  .alerts li:last-child {
    border-bottom-left-radius: var(--radius);
  }
  .alerts .level-warn {
    border-left-color: var(--crashed);
  }
  .alerts .level-error {
    border-left-color: var(--failed);
  }
  .artifacts li {
    padding: 10px 14px;
    overflow-wrap: anywhere;
  }
  .edge-row {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    align-items: baseline;
  }
  .artifacts code {
    display: block;
    margin-top: 2px;
    font-family: var(--mono);
  }
  .tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 2px;
    margin: 20px 0 16px;
    border-bottom: 1px solid var(--rule);
  }
  .tabs button {
    white-space: nowrap;
    border: 0;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    margin-bottom: -1px;
    padding: 8px 12px;
    background: none;
    color: var(--muted);
  }
  .tabs button:hover {
    color: var(--ink);
  }
  .tabs button[aria-selected='true'] {
    background: none;
    color: var(--ink);
    border-bottom-color: var(--ink);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
    gap: 12px;
  }
  .kv {
    display: grid;
    grid-template-columns: minmax(0, max-content) minmax(0, 1fr);
    gap: 0;
    margin: 0 0 24px;
    font-size: 0.875rem;
  }
  .kv dt,
  .kv dd {
    padding: 7px 14px;
    border-top: 1px solid var(--rule);
    overflow-wrap: anywhere;
  }
  .kv dt:first-child,
  .kv dt:first-child + dd {
    border-top: 0;
  }
  .kv dt {
    color: var(--muted);
    font-family: var(--mono);
  }
  .kv dd {
    margin: 0;
  }
</style>
