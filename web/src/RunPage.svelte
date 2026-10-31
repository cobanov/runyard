<script>
  import { get, onEvent, throttle, runTime, ago, live, plotValue, fmt, flatten } from './lib.svelte.js'
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
    ...(sysKeys.length ? [['system', 'GPU & system']] : []),
  ])
  const lineOf = (k) => {
    const s = series[k]
    return [{ label: k, color: '--c1', x: s.step, y: s.last.map(plotValue), min: s.min.map(plotValue), max: s.max.map(plotValue) }]
  }
  const bytes = (n) => (n < 1e3 ? `${n} B` : n < 1e6 ? `${(n / 1e3).toFixed(1)} kB` : n < 1e9 ? `${(n / 1e6).toFixed(1)} MB` : `${(n / 1e9).toFixed(2)} GB`)
  const config = $derived(Object.entries(flatten(run?.config)))
  const summary = $derived(Object.entries(flatten(run?.summary)))
</script>

{#if error}
  <p class="empty">Could not load this run: {error}</p>
{:else if run}
  <div class="status-{run.status}">
    <p class="muted small crumb"><a href="#/">Projects</a> / <a href="#/p/{encodeURIComponent(run.project)}">{run.project}</a> /</p>
    <div class="edge head">
      <h1>{run.name}</h1>
      <p class="small">
        <span class="state">{run.status}</span>
        <span class="muted">
          for {runTime(run, live.now)}, started {ago(run.started_at, live.now)}{run.host ? ` on ${run.host}` : ''}{#if run.status === 'running'}, last heartbeat {ago(run.last_heartbeat_at, live.now)}{/if}
        </span>
      </p>
      {#if run.git?.commit}
        <p class="small muted">git {run.git.branch} {run.git.commit.slice(0, 8)}{run.git.dirty ? ' with uncommitted changes' : ''}</p>
      {/if}
      {#if run.notes}<p class="small">{run.notes}</p>{/if}
    </div>
  </div>

  {#if alerts.length}
    <ul class="alerts small" aria-label="Alerts">
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
    <ul class="row-list artifacts">
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
      <dl>{#each summary as [k, v]}<dt>{k}</dt><dd>{fmt(v)}</dd>{/each}</dl>
    {/if}
    <h2>Config</h2>
    {#if config.length}
      <dl>{#each config as [k, v]}<dt>{k}</dt><dd>{fmt(v)}</dd>{/each}</dl>
    {:else}
      <p class="empty">This run was started without a config.</p>
    {/if}
  {/if}
{/if}

<style>
  .crumb {
    margin: 0 0 6px;
  }
  .head {
    padding: 2px 0 2px 14px;
  }
  .head p {
    margin: 4px 0 0;
  }
  .alerts {
    list-style: none;
    margin: 14px 0 0;
    padding: 0;
    display: grid;
    gap: 4px;
  }
  .alerts li {
    border-left: 3px solid var(--muted);
    padding: 2px 0 2px 10px;
    overflow-wrap: anywhere;
  }
  .alerts .level-warn {
    border-left-color: var(--crashed);
  }
  .alerts .level-error {
    border-left-color: var(--failed);
  }
  .artifacts li {
    padding: 10px 0;
    border-bottom: 1px solid var(--rule);
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
    gap: 6px;
    margin: 18px 0 14px;
    overflow-x: auto;
    padding-bottom: 2px;
  }
  .tabs button {
    white-space: nowrap;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
    gap: 12px;
  }
  dl {
    display: grid;
    grid-template-columns: minmax(0, max-content) minmax(0, 1fr);
    gap: 4px 16px;
    margin: 0 0 20px;
  }
  dt {
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
</style>
