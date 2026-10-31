<script>
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import { cssVar, fmt } from './lib.svelte.js'

  // lines: [{label, color (css var name), x, y, min?, max?}]. A single line with min/max draws the bucket range as a band.
  let { title, lines, xLabel = 'step', height = 170 } = $props()

  let el = $state()
  let hover = $state(null)
  let plot

  const last = $derived(lines.length === 1 ? lines[0].y.findLast((v) => v != null) : undefined)

  function data() {
    const l = lines[0]
    if (lines.length === 1) return l.min ? [l.x, l.y, l.min, l.max] : [l.x, l.y]
    return uPlot.join(lines.map((l) => [l.x, l.y]))
  }

  function build() {
    const muted = cssVar('--muted')
    const grid = { stroke: cssVar('--rule'), width: 1 }
    const font = `12px ${cssVar('--sans')}`
    const band = lines.length === 1 && lines[0].min
    const color = (l) => cssVar(l.color)
    const series = [{ label: xLabel }, ...lines.map((l) => ({ label: l.label, stroke: color(l), width: 1.5, points: { show: false }, spanGaps: true }))]
    if (band) series.push({ stroke: 'transparent', points: { show: false } }, { stroke: 'transparent', points: { show: false } })
    plot = new uPlot(
      {
        width: el.clientWidth,
        height,
        series,
        legend: { show: false },
        scales: { x: { time: false } },
        bands: band ? [{ series: [3, 2], fill: color(lines[0]) + '2e' }] : [],
        axes: [
          { stroke: muted, grid, ticks: { show: false }, font, size: 28 },
          { stroke: muted, grid, ticks: { show: false }, font, size: 52, values: (u, vals) => vals.map(fmt) },
        ],
        cursor: { points: { size: 6 } },
        hooks: {
          setCursor: [
            (u) => {
              const i = u.cursor.idx
              hover = i == null ? null : { x: u.data[0][i], ys: lines.map((_, j) => u.data[j + 1][i]) }
            },
          ],
        },
      },
      data(),
      el,
    )
  }

  $effect(() => {
    lines // rebuild when the data changes; uPlot redraws fast enough for live updates
    build()
    const ro = new ResizeObserver(() => plot.setSize({ width: el.clientWidth, height }))
    ro.observe(el)
    return () => {
      ro.disconnect()
      plot.destroy()
    }
  })
</script>

<figure>
  <figcaption>
    <span class="title">{title}</span>
    {#if hover}
      <span class="muted small">{xLabel} {fmt(hover.x)}{#if lines.length === 1}: <b>{hover.ys[0] == null ? '–' : fmt(hover.ys[0])}</b>{/if}</span>
    {:else if last !== undefined}
      <b>{fmt(last)}</b>
    {/if}
  </figcaption>
  <div bind:this={el} class="plot"></div>
</figure>

<style>
  figure {
    margin: 0;
    background: var(--panel);
    border: 1px solid var(--rule);
    border-radius: var(--radius);
    box-shadow: var(--shadow);
    padding: 12px 12px 6px;
    min-width: 0;
  }
  figcaption {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    align-items: baseline;
    padding: 0 4px 6px;
  }
  .title {
    font-weight: 600;
    font-size: 0.875rem;
    overflow-wrap: anywhere;
  }
  .plot {
    width: 100%;
  }
</style>
