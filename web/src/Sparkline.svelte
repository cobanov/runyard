<script>
  // A tiny trend line: the last values of one metric, no axes. Nulls (NaN values) leave gaps.
  let { values = [], color = '--c1', height = 36 } = $props()
  const w = 100
  const path = $derived.by(() => {
    const pts = values.map((v, i) => [i, v]).filter(([, v]) => v != null)
    if (pts.length < 2) return ''
    const ys = pts.map(([, v]) => v)
    const lo = Math.min(...ys)
    const span = Math.max(...ys) - lo || 1
    const n = values.length - 1
    return pts.map(([i, v], j) => `${j ? 'L' : 'M'}${((i / n) * w).toFixed(1)} ${(height - 2 - ((v - lo) / span) * (height - 4)).toFixed(1)}`).join(' ')
  })
</script>

<svg viewBox="0 0 {w} {height}" preserveAspectRatio="none" {height} aria-hidden="true">
  {#if path}<path d={path} fill="none" stroke="var({color})" stroke-width="1.5" vector-effect="non-scaling-stroke" stroke-linejoin="round" />{/if}
</svg>

<style>
  svg {
    display: block;
    width: 100%;
  }
</style>
