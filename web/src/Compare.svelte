<script>
  import { get, plotValue, fmt, flatten, palette } from './lib.svelte.js'
  import Chart from './Chart.svelte'

  let { ids } = $props()
  let runs = $state([])
  let series = $state([])

  $effect(() => {
    Promise.all(ids.map((id) => get('/runs/' + id))).then((r) => (runs = r))
    Promise.all(ids.map((id) => get(`/runs/${id}/metrics?points=600`))).then((s) => (series = s))
  })

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

<div class="grid">
  {#each keys as k (k)}
    <Chart title={k} lines={lines(k)} />
  {/each}
</div>

<h2>Config differences</h2>
{#if runs.length && !diff.length}
  <p class="empty">These runs have the same config.</p>
{:else if diff.length}
  <div class="card table">
    <table>
      <thead><tr><th></th>{#each runs as r, i}<th><span class="swatch" style="background: var({color(i)})"></span>{r.name}</th>{/each}</tr></thead>
      <tbody>
        {#each diff as [k, vals]}
          <tr><th>{k}</th>{#each vals as v}<td>{v === undefined ? '–' : fmt(v)}</td>{/each}</tr>
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
