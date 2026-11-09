<script>
  import { get, plotValue, flatten, palette, chartView as view } from './lib.svelte.js'
  import Chart from './Chart.svelte'

  let { ids: asked } = $props()
  let ids = $state(asked)
  let runs = $state([])
  let series = $state([])
  let media = $state([]) // per run, its images
  let gone = $state(0) // runs in the link that no longer exist

  $effect(() => {
    // A deleted run must not take the comparison of the others with it.
    Promise.allSettled(asked.map((id) => get('/runs/' + id))).then((res) => {
      const ok = res.filter((r) => r.status === 'fulfilled').map((r) => r.value)
      gone = res.length - ok.length
      runs = ok
      ids = ok.map((r) => r.id)
      Promise.all(ids.map((id) => get(`/runs/${id}/metrics?points=600`))).then((s) => (series = s))
      Promise.all(ids.map((id) => get(`/runs/${id}/media?kind=image`))).then((m) => (media = m))
    })
  })

  // Samples side by side: for each key any run logged images under, one row, a column per run, each showing its
  // images at the step nearest below the chosen one. One slider moves them all.
  const sampleKeys = $derived([...new Set(media.flat().map((m) => m.key))].sort())
  const steps = $derived([...new Set(media.flat().map((m) => m.step))].sort((a, b) => a - b))
  let stepAt = $state(null) // index into steps; null follows the last
  const step = $derived(steps[stepAt ?? steps.length - 1])
  function at(i, key) {
    const mine = (media[i] || []).filter((m) => m.key === key && m.step <= step)
    const last = Math.max(...mine.map((m) => m.step))
    return mine.filter((m) => m.step === last)
  }

  const color = (i) => palette[i % palette.length]
  const keys = $derived([...new Set(series.flatMap((s) => Object.keys(s)))].filter((k) => !k.startsWith('sys/')).sort())
  const lines = (k) =>
    series.flatMap((s, i) => (s[k] ? [{ label: runs[i]?.name ?? ids[i], color: color(i), x: s[k].step, y: s[k].last.map(plotValue) }] : []))

  // Only the config keys whose values differ between the runs.
  const diff = $derived.by(() => {
    const flat = runs.map((r) => flatten(r.config))
    const all = [...new Set(flat.flatMap(Object.keys))].sort()
    return all.filter((k) => new Set(flat.map((f) => JSON.stringify(f[k]))).size > 1).map((k) => [k, flat.map((f) => f[k])])
  })
</script>

<p class="crumb"><a href="#/projects">Projects</a> /{#if runs[0]}&nbsp;<a href="#/p/{encodeURIComponent(runs[0].project)}">{runs[0].project}</a> /{/if}</p>
<div class="page-head">
  <h1>Comparing {ids.length} runs</h1>
  <ul class="legend">
    {#each runs as r, i (r.id)}
      <li class="status-{r.status}"><span class="swatch" style="background: var({color(i)})"></span><a href="#/r/{r.id}">{r.name}</a> <span class="state small">{r.status}</span></li>
    {/each}
  </ul>
</div>

{#if gone}<p class="empty">{gone === 1 ? 'One run in this link no longer exists' : `${gone} runs in this link no longer exist`}; comparing the rest.</p>{/if}
{#if keys.length}
  <div class="view">
    <label class="small muted"
      >Smoothing <input type="range" min="0" max="0.99" step="0.01" bind:value={view.smooth} /><output>{view.smooth ? view.smooth.toFixed(2) : 'off'}</output></label
    >
    <button aria-pressed={view.log} onclick={() => (view.log = !view.log)}>Log scale</button>
  </div>
{/if}
<div class="grid">
  {#each keys as k (k)}
    <Chart title={k} lines={lines(k)} smooth={view.smooth} log={view.log} />
  {/each}
</div>

{#if sampleKeys.length}
  <div class="bar">
    <h2>Samples</h2>
    <span class="muted small">up to step {step.toLocaleString('en-US')}</span>
  </div>
  {#if steps.length > 1}
    <input class="steps" type="range" min="0" max={steps.length - 1} value={stepAt ?? steps.length - 1} oninput={(e) => (stepAt = +e.target.value)} aria-label="Step for the samples of every run" />
  {/if}
  <div class="card table samples">
    <table>
      <thead><tr><th></th>{#each runs as r, i}<th><span class="swatch" style="background: var({color(i)})"></span>{r.name}</th>{/each}</tr></thead>
      <tbody>
        {#each sampleKeys as key (key)}
          <tr>
            <th>{key}</th>
            {#each runs as r, i (r.id)}
              <td>
                {#each at(i, key) as m (m.id)}
                  <a href={m.url} target="_blank" rel="noopener" title="{m.meta.caption || key}, step {m.step}"><img src={m.url} alt="{m.meta.caption || key} of {r.name} at step {m.step}" loading="lazy" /></a>
                {:else}
                  <span class="muted">–</span>
                {/each}
              </td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<h2>Config differences</h2>
{#if runs.length && !diff.length}
  <p class="empty">These runs have the same config.</p>
{:else if diff.length}
  <div class="card table">
    <table>
      <thead><tr><th></th>{#each runs as r, i}<th><span class="swatch" style="background: var({color(i)})"></span>{r.name}</th>{/each}</tr></thead>
      <tbody>
        {#each diff as [k, vals]}
          <tr><th>{k}</th>{#each vals as v}<td>{v === undefined ? '–' : String(v)}</td>{/each}</tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .legend {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 6px 18px;
    font-size: 0.875rem;
  }
  .legend a {
    text-decoration: none;
    font-weight: 600;
  }
  .swatch {
    display: inline-block;
    width: 12px;
    height: 12px;
    border-radius: 3px;
    margin-right: 6px;
    vertical-align: -1px;
  }
  .view {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px 16px;
    margin: 0 0 12px;
  }
  .view label {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .view input,
  .steps {
    accent-color: var(--ink);
  }
  .view output {
    min-width: 3ch;
    color: var(--ink);
  }
  .bar {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
  }
  .steps {
    width: 100%;
    margin: 0 0 12px;
  }
  .samples {
    margin-bottom: 28px;
  }
  .samples td {
    vertical-align: top;
  }
  .samples img {
    display: inline-block;
    width: min(220px, 100%);
    border-radius: var(--radius-sm);
    margin: 0 4px 4px 0;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
    gap: 12px;
    margin-bottom: 28px;
  }
  .table {
    overflow-x: auto;
  }
  tbody th {
    color: var(--muted);
    font-weight: 400;
    font-family: var(--mono);
    font-size: 0.8125rem;
    border-bottom: 1px solid var(--rule);
  }
  tbody tr:last-child th {
    border-bottom: 0;
  }
</style>
