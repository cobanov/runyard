<script>
  import { get, send, why, onEvent, throttle, ago, live } from './lib.svelte.js'
  import Chart from './Chart.svelte'

  // Each machine running `runyard agent`: its latest reading, the runs training on it, and every process on its GPUs.
  let hosts = $state(null)
  let error = $state('')
  const load = throttle(
    () => get('/hosts').then((h) => ((hosts = h), (error = '')), (e) => (error = e.message)),
    3000,
  )
  load()
  $effect(() => onEvent((e) => (e.type === 'host' || e.type === 'run' || e.type === 'reconnect') && load()))

  // History: refetched every 30 s and when the window changes, not on every reading.
  let hours = $state(1)
  let hist = $state({}) // host name -> {ts, values}
  let totals = $state({}) // host name -> its stats when the history came, for the memory charts' scale
  const names = $derived(hosts?.map((h) => h.name).join('\n') || '')
  $effect(() => {
    const list = names ? names.split('\n') : []
    const h = hours
    const load = async () => {
      const out = {}
      await Promise.all(list.map(async (n) => (out[n] = await get(`/hosts/${encodeURIComponent(n)}/history?hours=${h}`).catch(() => null))))
      totals = Object.fromEntries((hosts || []).map((x) => [x.name, x.stats])) // after the await: not a dependency of this effect
      hist = out
    }
    load()
    const t = setInterval(load, 30000)
    return () => clearInterval(t)
  })
  const charts = $derived(Object.fromEntries(Object.entries(hist).map(([n, h]) => [n, h?.ts.length ? chartsOf(h, totals[n] || {}) : []])))
  function chartsOf(h, st) {
    const gpuTotal = (i) => st.gpus?.find((g) => g.i === +i)?.mem_total_gb
    const gpus = [...new Set(Object.keys(h.values).map((k) => k.match(/^gpu(\d+)\./)?.[1]).filter(Boolean))].sort((a, b) => a - b)
    const want = [
      ['cpu', 'CPU', '%', '--c3', 100],
      ['ram_gb', 'Memory', 'GB', '--c4', st.ram_total_gb],
      ...gpus.flatMap((i) => [[`gpu${i}.util`, `GPU ${i} util`, '%', '--c1', 100], [`gpu${i}.mem_gb`, `GPU ${i} memory`, 'GB', '--c2', gpuTotal(i)]]),
    ]
    // Against the whole (0 to 100 %, 0 to all the memory), so a quiet machine looks quiet.
    return want
      .filter(([k]) => h.values[k])
      .map(([k, label, unit, color, top]) => ({ k, title: `${label}, ${unit}`, range: top ? [0, top] : null, lines: [{ label, color, x: h.ts, y: h.values[k] }] }))
  }

  const remove = (h) =>
    confirm(`Remove ${h.name}? It comes back when its agent sends again.`) &&
    send('DELETE', '/hosts/' + encodeURIComponent(h.name)).then(load, (e) => (error = why(e)))

  const pct = (v, total) => (typeof v === 'number' && total ? Math.max(0, Math.min(100, (100 * v) / total)) : 0)
  // Glanced at, not compared: whole numbers, one decimal for gigabytes.
  const n0 = (v, unit = '') => (typeof v === 'number' ? `${Math.round(v)}${unit}` : '–')
  const n1 = (v, unit = '') => (typeof v === 'number' ? `${v.toFixed(1)}${unit}` : '–')
  const of = (used, total, unit) => (total ? `${n1(used)} / ${n1(total, unit)}` : n1(used, unit))
</script>

{#snippet bar(p)}
  <span class="track" class:hot={p >= 90}><span class="fill" style="width: {p}%"></span></span>
{/snippet}

<div class="page-head">
  <h1>Machines</h1>
  {#if hosts?.length}
    <span class="muted small">{hosts.filter((h) => !h.silent).length} of {hosts.length} sending</span>
    <div class="range" role="group" aria-label="History">
      {#each [1, 6, 24] as n}<button aria-pressed={hours === n} onclick={() => (hours = n)}>{n}h</button>{/each}
    </div>
  {/if}
</div>

{#if error}
  <p class="empty"><span class="danger">Failed.</span> {error}</p>
{/if}
{#if hosts?.length === 0}
  <div class="card card-pad intro">
    <p>No machines yet. Run the agent on each machine you train on: it sends GPU, CPU, memory and disk every 10 seconds, training or not, and lists every process on the GPUs, whoever started it.</p>
    <pre class="small">pip install "runyard[system]"
RUNYARD_SERVER={location.origin} RUNYARD_API_KEY=ry_... runyard agent</pre>
    <p class="small muted">To keep it running, install it as a service: <a href="https://cobanov.github.io/runyard/server/#machines">Run the server, Machines</a>. The server tells your channels when a machine goes silent.</p>
  </div>
{:else if hosts}
  <div class="hosts">
    {#each hosts as h (h.name)}
      {@const s = h.stats}
      <article class="card card-pad host" class:silent={h.silent}>
        <header>
          <h2><span class="dot" title={h.silent ? 'Silent' : 'Sending'}></span>{h.name}</h2>
          <span class="muted small">{h.silent ? 'silent, last heard' : 'updated'} {ago(h.seen_at, live.now)}</span>
          {#if h.silent}<button class="danger" onclick={() => remove(h)}>Remove</button>{/if}
        </header>

        <dl class="meters">
          {#if s.cpu != null}
            <dt>CPU</dt>
            <dd>{@render bar(pct(s.cpu, 100))}{n0(s.cpu, '%')}<span class="muted small">{#if s.cores} of {s.cores} cores{/if}{#if s.load != null}, load {s.load.toFixed(1)}{/if}</span></dd>
          {/if}
          {#if s.ram_gb != null}
            <dt>Memory</dt>
            <dd>{@render bar(pct(s.ram_gb, s.ram_total_gb))}{of(s.ram_gb, s.ram_total_gb, ' GB')}</dd>
          {/if}
          {#if s.disk_gb != null}
            <dt>Disk</dt>
            <dd>{@render bar(pct(s.disk_gb, s.disk_total_gb))}{of(s.disk_gb, s.disk_total_gb, ' GB')}</dd>
          {/if}
        </dl>

        {#if h.runs.length}
          <p class="small training">
            Training:
            {#each h.runs as r, i (r.id)}{i ? ', ' : ' '}<a href="#/r/{r.id}">{r.name}</a> <span class="muted">{r.project}</span>{/each}
          </p>
        {/if}

        {#if charts[h.name]?.length}
          <div class="charts">
            {#each charts[h.name] as c (c.k)}<Chart title={c.title} lines={c.lines} range={c.range} xLabel="at" height={110} time />{/each}
          </div>
        {/if}

        {#each s.gpus || [] as g (g.i)}
          <section class="gpu">
            <h3>GPU {g.i} <span class="muted small">{g.name || ''}</span></h3>
            <dl class="meters">
              <dt>Util</dt>
              <dd>{@render bar(pct(g.util, 100))}{n0(g.util, '%')}</dd>
              <dt>Memory</dt>
              <dd>{@render bar(pct(g.mem_gb, g.mem_total_gb))}{of(g.mem_gb, g.mem_total_gb, ' GB')}</dd>
              <dt>Temp, power</dt>
              <dd>{n0(g.temp_c, ' °C')}, {n0(g.power_w)}{g.power_max_w ? ` / ${n0(g.power_max_w)}` : ''} W</dd>
            </dl>
            {#if g.procs?.length}
              <div class="table">
                <table>
                  <thead><tr><th>Process</th><th>User</th><th>PID</th><th class="num">Memory</th></tr></thead>
                  <tbody>
                    {#each g.procs as p (p.pid)}
                      <tr><td>{p.name || '?'}</td><td>{p.user || '–'}</td><td class="muted">{p.pid}</td><td class="num">{n1(p.mem_gb, ' GB')}</td></tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {:else if g.procs}
              <p class="small muted idle">No processes: free.</p>
            {/if}
          </section>
        {/each}
      </article>
    {/each}
  </div>
{/if}

<style>
  .intro {
    display: grid;
    gap: 10px;
    max-width: 72ch;
  }
  .intro p,
  .intro pre {
    margin: 0;
  }
  .intro pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .hosts {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 460px), 1fr));
    gap: 14px;
    align-items: start;
  }
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 4px 12px;
    margin-bottom: 12px;
  }
  header button {
    margin-left: auto;
    padding: 2px 10px;
  }
  h2 {
    display: flex;
    align-items: center;
    gap: 8px;
    margin: 0;
    font-size: 1.0625rem;
    font-weight: 700;
    color: var(--ink);
    overflow-wrap: anywhere;
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--running);
    flex: none;
  }
  .silent .dot {
    background: var(--crashed);
  }
  .silent .meters,
  .silent .gpu {
    opacity: 0.55; /* the last reading, no longer current */
  }
  h3 {
    margin: 0 0 6px;
    font-size: 0.875rem;
    font-weight: 600;
  }
  .meters {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 4px 14px;
    margin: 0;
    font-size: 0.875rem;
    align-items: center;
  }
  dt {
    color: var(--muted);
    font-size: 0.8125rem;
  }
  dd {
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0 6px;
  }
  .track {
    display: inline-block;
    width: 96px;
    height: 6px;
    margin-right: 4px;
    border-radius: 3px;
    background: var(--soft);
    overflow: hidden;
  }
  .fill {
    display: block;
    height: 100%;
    background: var(--c2);
  }
  .hot .fill {
    background: var(--crashed);
  }
  .training {
    margin: 12px 0 0;
  }
  .range {
    display: flex;
    gap: 4px;
    margin-left: auto;
  }
  .range button {
    padding: 2px 10px;
  }
  .charts {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 190px), 1fr));
    gap: 8px;
    margin-top: 14px;
  }
  .gpu {
    margin-top: 14px;
    padding-top: 12px;
    border-top: 1px solid var(--rule);
  }
  .gpu .table {
    margin-top: 8px;
  }
  .gpu th,
  .gpu td {
    padding: 5px 8px;
  }
  .gpu th:first-child,
  .gpu td:first-child {
    padding-left: 0;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .num {
    text-align: right;
  }
  .idle {
    margin: 8px 0 0;
  }
  @media (max-width: 479px) {
    .track {
      width: 56px;
    }
  }
</style>
