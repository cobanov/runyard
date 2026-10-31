<script>
  import { get, onEvent, throttle, ago, runTime, live, fmt, plotValue, mainMetric, sysLoad } from './lib.svelte.js'
  import Sparkline from './Sparkline.svelte'

  let running = $state(null) // full runs, with their metric summaries
  let spark = $state({}) // run id -> last values of its main metric
  let recent = $state([])
  let alerts = $state([])
  let error = $state('')

  const loadRunning = throttle(async () => {
    try {
      const full = await get('/runs?status=running&limit=50&metrics=1')
      const sp = {}
      await Promise.all(
        full.map(async (r) => {
          const k = mainMetric(r.metrics)
          if (!k) return
          const s = await get(`/runs/${r.id}/metrics?points=40&keys=${encodeURIComponent(k)}`)
          sp[r.id] = s[k]?.last.map(plotValue) || []
        }),
      )
      running = full
      spark = sp
      error = ''
    } catch (e) {
      error = e.message
    }
  }, 4000)
  const loadRecent = throttle(() => get('/runs?limit=40').then((r) => (recent = r.filter((x) => x.status !== 'running').slice(0, 8))))
  const loadAlerts = throttle(() => get('/alerts?limit=5').then((a) => (alerts = a)))
  loadRunning()
  loadRecent()
  loadAlerts()
  $effect(() =>
    onEvent((e) => {
      if (e.type === 'run' || e.type === 'reconnect') (loadRunning(), loadRecent(), loadAlerts())
      if (e.type === 'metrics') loadRunning()
      if (e.type === 'alert') loadAlerts()
    }),
  )

  const hosts = $derived(sysLoad(running || []))
  const pct = (v) => (typeof v === 'number' ? Math.max(0, Math.min(100, v)) : 0)
  // Machine readings are glanced at, not compared: whole numbers, one decimal for gigabytes.
  const n0 = (v, unit) => (typeof v === 'number' ? `${Math.round(v)}${unit}` : '–')
  const n1 = (v, unit) => (typeof v === 'number' ? `${v.toFixed(1)}${unit}` : '–')
</script>

<div class="page-head">
  <h1>Overview</h1>
  {#if running}<span class="muted small">{running.length} running</span>{/if}
</div>

{#if error}
  <p class="empty">Could not load runs: {error}</p>
{:else if running?.length === 0}
  <div class="card card-pad quiet">
    <p class="muted">Nothing is training right now.</p>
  </div>
{:else if running}
  <div class="cards">
    {#each running as r (r.id)}
      {@const k = mainMetric(r.metrics)}
      <a href="#/r/{r.id}" class="card run status-{r.status}">
        <div class="who">
          <span class="muted small">{r.project}</span>
          <span class="badge">{r.status}</span>
        </div>
        <span class="name">{r.name}</span>
        {#if k}
          <div class="metric">
            <span class="value">{fmt(r.metrics[k].last)}</span>
            <span class="muted small">{k} at step {fmt(r.metrics[k].step)}</span>
          </div>
          <Sparkline values={spark[r.id] || []} color="--c2" />
        {:else}
          <p class="muted small nometric">No metrics logged yet.</p>
        {/if}
        <div class="meta muted small">
          <span>{runTime(r, live.now)}</span>
          <span>heartbeat {ago(r.last_heartbeat_at, live.now)}</span>
          {#if r.host}<span>{r.host}</span>{/if}
        </div>
      </a>
    {/each}
  </div>
{/if}

<section>
  <h2>Machine load</h2>
  {#if !hosts.length}
    <p class="empty">Shows GPU, CPU and memory from running runs that log <code>sys/</code> metrics.</p>
  {:else}
    <div class="hosts">
      {#each hosts as h (h.host)}
        <div class="card card-pad host">
          <div class="who">
            <strong>{h.host}</strong>
            <span class="muted small">
              {#if h.cpu != null}CPU {n0(h.cpu, '%')}{/if}{#if h.ram_gb != null}, RAM {n1(h.ram_gb, ' GB')}{/if}
            </span>
          </div>
          {#if h.gpus.length}
            <div class="table"><table class="gpus">
              <thead><tr><th>GPU</th><th class="bar">Util</th><th>Memory</th><th>Temp</th><th>Power</th></tr></thead>
              <tbody>
                {#each h.gpus as g (g.i)}
                  <tr>
                    <td>{g.i}</td>
                    <td class="bar"><span class="track"><span class="fill" style="width: {pct(g.util)}%"></span></span>{n0(g.util, '%')}</td>
                    <td>{n1(g.mem_gb, ' GB')}</td>
                    <td>{n0(g.temp_c, ' °C')}</td>
                    <td>{n0(g.power_w, ' W')}</td>
                  </tr>
                {/each}
              </tbody>
            </table></div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</section>

<div class="two">
  <section>
    <h2>Recently ended</h2>
    {#if !recent.length}
      <p class="empty">No finished runs yet.</p>
    {:else}
      <ul class="card row-list">
        {#each recent as r (r.id)}
          <li class="status-{r.status}">
            <a href="#/r/{r.id}" class="line">
              <span class="badge" title={r.status}></span>
              <span class="grow"><span class="name">{r.name}</span> <span class="muted small">{r.project}</span></span>
              <span class="muted small">{runTime(r, live.now)}, {ago(r.ended_at || r.started_at, live.now)}</span>
            </a>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
  <section>
    <h2>Alerts</h2>
    {#if !alerts.length}
      <p class="empty">No alerts. Runs raise them when they stall, fail or call <code>run.alert()</code>.</p>
    {:else}
      <ul class="card row-list">
        {#each alerts as a (a.id)}
          <li class="level-{a.level}">
            <a href="#/r/{a.run}" class="line">
              <span class="grow"><span class="name">{a.title}</span> <span class="muted small">{a.run_name}</span></span>
              <span class="muted small">{ago(a.ts, live.now)}</span>
            </a>
          </li>
        {/each}
      </ul>
      <p class="small more"><a href="#/alerts">All alerts</a></p>
    {/if}
  </section>
</div>

<style>
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 280px), 1fr));
    gap: 14px;
  }
  .run {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 16px 18px 14px;
    text-decoration: none;
  }
  .run:hover {
    border-color: var(--muted);
  }
  .who {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    align-items: baseline;
  }
  .name {
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .metric {
    display: flex;
    flex-direction: column;
    margin-top: 8px;
  }
  .value {
    font-size: 1.75rem;
    font-weight: 700;
    letter-spacing: -0.02em;
    line-height: 1.1;
  }
  .nometric {
    margin: 10px 0 18px;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    margin-top: 8px;
  }
  .quiet p {
    margin: 0;
  }
  section {
    margin-top: 32px;
  }
  section .empty {
    padding: 8px 14px;
    border: 1px dashed var(--rule);
    border-radius: var(--radius);
    max-width: none;
  }
  .hosts {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 420px), 1fr));
    gap: 14px;
  }
  .host .who {
    margin-bottom: 8px;
  }
  .gpus th,
  .gpus td {
    padding: 6px 8px;
  }
  .gpus th:first-child,
  .gpus td:first-child {
    padding-left: 0;
  }
  .track {
    display: inline-block;
    vertical-align: middle;
    width: 72px;
    height: 6px;
    margin-right: 8px;
    border-radius: 3px;
    background: var(--soft);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    background: var(--c2);
  }
  @media (max-width: 479px) {
    .track {
      display: none; /* the number alone fits a phone; the bar does not */
    }
    .gpus th,
    .gpus td {
      padding: 6px 5px;
    }
  }
  .two {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(min(100%, 380px), 1fr));
    gap: 0 24px;
  }
  .line {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    text-decoration: none;
    font-size: 0.875rem;
  }
  .line:hover {
    background: var(--soft);
  }
  .grow {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0 6px;
    overflow-wrap: anywhere;
  }
  .level-warn .name {
    color: var(--crashed);
  }
  .level-error .name {
    color: var(--failed);
  }
  .more {
    margin: 8px 2px 0;
  }
</style>
