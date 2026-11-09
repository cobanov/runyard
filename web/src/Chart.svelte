<script>
  import { untrack } from 'svelte'
  import uPlot from 'uplot'
  import 'uplot/dist/uPlot.min.css'
  import { cssVar, cssColor, withAlpha, theme, fmt, duration } from './lib.svelte.js'

  // lines: [{label, color (css var name), x, y, min?, max?}]. A single line with min/max draws the bucket range as a band.
  // smooth: EMA weight, 0 is off; log: a log10 y axis; elapsed: x is seconds since the run started; time: x is unix seconds;
  // range: a fixed [min, max] for y, as 0 to 100 for a percentage.
  let { title, lines, xLabel = 'step', height = 170, smooth = 0, log = false, elapsed = false, time = false, range = null } = $props()

  let el = $state()
  let hover = $state(null)
  let plot
  let zoomed = false // the reader dragged a range: new points must not throw it away

  const last = $derived(lines.length === 1 ? lines[0].y.findLast((v) => v != null) : undefined)
  const xFmt = (x) =>
    time ? new Date(x * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false }) : elapsed ? duration(x * 1000) : fmt(x)

  // TensorBoard's debiased EMA, so the start isn't dragged towards zero; gaps (null) pass through.
  function ema(ys, w) {
    let acc = 0
    let n = 0
    return ys.map((v) => (v == null ? null : ((acc = acc * w + (1 - w) * v), acc / (1 - w ** ++n))))
  }

  // With smoothing each line is two series: the raw one, then its EMA on top.
  function data() {
    const pos = (ys) => (log ? ys.map((v) => (v > 0 ? v : null)) : ys) // a log axis has no place for zero or below
    const ys = (l) => (smooth ? [l.y, ema(l.y, smooth)] : [l.y]).map(pos)
    const l = lines[0]
    if (lines.length === 1) return [l.x, ...ys(l), ...(l.min ? [pos(l.min), pos(l.max)] : [])]
    return uPlot.join(lines.map((l) => [l.x, ...ys(l)]))
  }

  function build() {
    const muted = cssColor('--muted')
    const grid = { stroke: cssColor('--rule'), width: 1 }
    const font = `12px ${cssVar('--sans')}`
    const band = lines.length === 1 && lines[0].min
    const color = (l) => cssColor(l.color)
    const per = smooth ? 2 : 1
    const series = [
      { label: xLabel },
      ...lines.flatMap((l) => {
        const line = { label: l.label, stroke: color(l), width: 1.5, points: { show: false }, spanGaps: true }
        return smooth ? [{ ...line, stroke: withAlpha(color(l), 0.3), width: 1 }, line] : [line]
      }),
    ]
    if (band) series.push({ stroke: 'transparent', points: { show: false } }, { stroke: 'transparent', points: { show: false } })
    plot = new uPlot(
      {
        width: el.clientWidth,
        height,
        series,
        legend: { show: false },
        scales: { x: { time }, y: { distr: log ? 3 : 1, ...(range && { range }) } },
        bands: band ? [{ series: [per + 2, per + 1], fill: withAlpha(color(lines[0]), 0.14) }] : [],
        axes: [
          { stroke: muted, grid, ticks: { show: false }, border: { show: false }, font, size: 28, ...(elapsed && { incrs: secs, values: (u, v) => v.map(short) }), ...(time && { values: (u, v) => v.map(xFmt) }) },
          { stroke: muted, grid, ticks: { show: false }, border: { show: false }, font, size: 52, values: (u, vals) => vals.map((v) => (v == null ? '' : fmt(v))), ...(log && { splits: decades }) },
        ],
        cursor: { points: { size: 6 } },
        hooks: {
          setSelect: [(u) => u.select.width > 0 && (zoomed = true)],
          setCursor: [
            (u) => {
              const i = u.cursor.idx
              hover = i == null ? null : { x: u.data[0][i], ys: lines.map((_, j) => u.data[(j + 1) * per][i]), raw: smooth ? u.data[1][i] : null }
            },
          ],
        },
      },
      data(),
      el,
    )
  }

  // Elapsed seconds tick at round minutes and hours.
  const secs = [1, 5, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400]
  const short = (s) => (s < 60 ? `${s}s` : s < 3600 ? `${+(s / 60).toFixed(1)}m` : `${+(s / 3600).toFixed(1)}h`)
  // A log axis gets one gridline per power of ten (uPlot widens its range to whole decades), not nine.
  function decades(u, i, min, max) {
    const out = []
    for (let p = Math.ceil(Math.log10(min)); p <= Math.floor(Math.log10(max)); p++) out.push(10 ** p)
    return out
  }

  // The plot is built again only when its shape changes: which lines, smoothing on or off, the axis, the theme.
  const shape = $derived(JSON.stringify([lines.map((l) => [l.label, l.color, !!l.min]), !!smooth, log, theme.dark, height, elapsed, time]))
  $effect(() => {
    shape
    untrack(build)
    zoomed = false
    const over = plot.over
    const reset = () => (zoomed = false) // a double click is uPlot's "show everything again"
    over.addEventListener('dblclick', reset)
    const ro = new ResizeObserver(() => plot.setSize({ width: el.clientWidth, height }))
    ro.observe(el)
    return () => {
      ro.disconnect()
      over.removeEventListener('dblclick', reset)
      plot.destroy()
      plot = null
    }
  })
  // New points, or another smoothing weight, go into the plot that is there.
  $effect(() => {
    const d = data()
    if (!plot) return
    plot.setData(d, !zoomed)
    if (zoomed) plot.redraw()
  })
</script>

<figure>
  <figcaption>
    <span class="title">{title}</span>
    {#if hover}
      <span class="muted small"
        >{xLabel} {xFmt(hover.x)}{#if lines.length === 1}: <b>{hover.ys[0] == null ? '–' : fmt(hover.ys[0])}</b>{#if hover.raw != null}, raw {fmt(hover.raw)}{/if}{:else}{#each lines as l, i}<span class="at"><i style="background: var({l.color})"></i><b>{hover.ys[i] == null ? '–' : fmt(hover.ys[i])}</b></span>{/each}{/if}</span
      >
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
  .at {
    margin-left: 10px;
    white-space: nowrap;
  }
  .at i {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 2px;
    margin-right: 4px;
  }
</style>
