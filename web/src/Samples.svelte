<script>
  import { get, onEvent, throttle, thumb } from './lib.svelte.js'
  import TextTable from './TextTable.svelte'

  let { id } = $props()
  let media = $state(null)
  let pick = $state({}) // key -> chosen step index; unset follows the latest step

  const load = throttle(() => get(`/runs/${id}/media`).then((m) => (media = m)), 3000)
  load()
  $effect(() => onEvent((e) => (e.type === 'reconnect' || (e.run === id && e.type === 'media')) && load()))

  // key -> {kind, steps: [step...], at: {step: [media...]}}
  const groups = $derived.by(() => {
    const g = {}
    for (const m of media || []) {
      const k = (g[m.key] ||= { kind: m.kind, steps: [], at: {} })
      if (!k.at[m.step]) (k.steps.push(m.step), (k.at[m.step] = []))
      k.at[m.step].push(m)
    }
    for (const k of Object.values(g)) k.steps.sort((a, b) => a - b)
    return g
  })

  // Images and videos of every key move together: one scrubber over all the steps any of them was logged at, so
  // every prompt is seen at the same moment of the training. A key that skipped a step shows its last one before it.
  const visual = $derived(Object.entries(groups).filter(([, g]) => g.kind !== 'text'))
  const texts = $derived(Object.entries(groups).filter(([, g]) => g.kind === 'text'))
  const steps = $derived([...new Set(visual.flatMap(([, g]) => g.steps))].sort((a, b) => a - b))
  let at = $state(null) // index into steps; null follows the newest
  const idx = $derived(Math.min(at ?? Infinity, steps.length - 1))
  const step = $derived(steps[idx])
  const jump = (i) => (at = i >= steps.length - 1 ? null : Math.max(0, i))
  const move = (d) => jump(idx + d)
  const shown = (g) => g.at[g.steps.findLast((x) => x <= step)] || []

  function stepOf(key) {
    const g = groups[key]
    return g.steps[pick[key] ?? g.steps.length - 1]
  }

  // Small images (pixel art, tiny diffusion samples) stay crisp when scaled up.
  const crisp = (e) => e.target.classList.toggle('crisp', e.target.naturalWidth < 256)
  const isImage = (m) => m.kind !== 'video' || m.url.endsWith('.gif')

  // The viewer follows one image through the steps: left and right move in time and keep its place in the step
  // (the same prompt), up and down move to the step's other images.
  let dialog
  let view = $state(null) // {key, s: index into steps, i: index within the step}
  const imagesAt = (s) => (view ? (groups[view.key].at[groups[view.key].steps[s]] || []).filter(isImage) : [])
  const list = $derived(view ? imagesAt(view.s) : [])
  const cur = $derived(list[Math.min(view?.i ?? 0, list.length - 1)])
  const count = $derived(view ? groups[view.key].steps.length : 0)
  function show(key, m) {
    const g = groups[key]
    view = { key, s: g.steps.indexOf(m.step), i: g.at[m.step].filter(isImage).findIndex((x) => x.id === m.id) }
    dialog.showModal()
  }
  const go = (d) => view && imagesAt(view.s + d).length && (view.s += d)
  const within = (d) => view && list[view.i + d] && (view.i += d)
  const keys = (e) =>
    e.key === 'ArrowLeft' ? go(-1) : e.key === 'ArrowRight' ? go(1) : e.key === 'ArrowUp' ? within(-1) : e.key === 'ArrowDown' && within(1)
</script>

{#if media?.length === 0}
  <p class="empty">No samples yet. Log them with <code>run.image()</code>, <code>run.video()</code> or <code>run.text()</code>.</p>
{/if}
{#if steps.length}
  <div class="scrub">
    <button onclick={() => move(-1)} disabled={idx === 0} aria-label="Earlier step">←</button>
    <input type="range" min="0" max={steps.length - 1} value={idx} oninput={(e) => jump(+e.target.value)} disabled={steps.length < 2} aria-label="Step of the samples" />
    <button onclick={() => move(1)} disabled={idx === steps.length - 1} aria-label="Later step">→</button>
    <span class="where small"><b>step {step.toLocaleString('en-US')}</b> <span class="muted">{idx + 1} of {steps.length}</span></span>
  </div>
  <div class="gallery">
    {#each visual as [key, g] (key)}
      {#each shown(g) as m (m.id)}
        <figure>
          {#if !isImage(m)}
            <video src={m.url} controls loop muted playsinline></video>
          {:else}
            <button class="thumb" onclick={() => show(key, m)} aria-label="View {m.meta.caption || key} large"
              ><img src={thumb(m.url)} alt={m.meta.caption || key} loading="lazy" onload={crisp} /></button
            >
          {/if}
          <figcaption class="small">
            <span class="key">{key}</span>{#if m.step !== step}<span class="muted"> from step {m.step.toLocaleString('en-US')}</span>{/if}
            {#if m.meta.caption}<span class="muted cap">{m.meta.caption}</span>{/if}
          </figcaption>
        </figure>
      {/each}
    {/each}
  </div>
{/if}
{#each texts as [key, g] (key)}
  {@const step = stepOf(key)}
  <section>
    <div class="bar">
      <h2>{key}</h2>
      <span class="muted small">step {step.toLocaleString('en-US')}{g.steps.length > 1 ? `, ${g.steps.length} steps logged` : ''}</span>
    </div>
    {#if g.steps.length > 1}
      <input
        type="range"
        min="0"
        max={g.steps.length - 1}
        value={pick[key] ?? g.steps.length - 1}
        oninput={(e) => (pick[key] = +e.target.value)}
        aria-label="Step for {key}"
      />
    {/if}
    {#each g.at[step] as m (m.id)}
      <TextTable url={m.url} />
    {/each}
  </section>
{/each}

<dialog bind:this={dialog} aria-label="Sample viewer" onclose={() => (view = null)} onclick={(e) => e.target === dialog && dialog.close()} onkeydown={keys}>
  {#if cur}
    <img src={cur.url} alt={cur.meta.caption || cur.key} onload={crisp} />
    <div class="info">
      <div>
        <strong>{cur.key}</strong>
        <span class="muted">step {cur.step.toLocaleString('en-US')}, {view.s + 1} of {count}{list.length > 1 ? `, image ${Math.min(view.i, list.length - 1) + 1} of ${list.length} (↑ ↓)` : ''}</span>
        {#if cur.meta.caption}<p class="small muted">{cur.meta.caption}</p>{/if}
      </div>
      <div class="nav">
        <a class="small" href={cur.url} target="_blank" rel="noopener">Open original</a>
        <button onclick={() => go(-1)} disabled={!view.s} aria-label="Earlier step">←</button>
        <button onclick={() => go(1)} disabled={view.s === count - 1} aria-label="Later step">→</button>
        <button onclick={() => dialog.close()}>Close</button>
      </div>
    </div>
  {/if}
</dialog>

<style>
  section {
    margin-bottom: 28px;
  }
  .bar {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 8px;
  }
  input[type='range'] {
    width: 100%;
    margin: 4px 0 12px;
    accent-color: var(--ink);
  }
  /* The scrubber stays in reach while the images scroll under it. */
  .scrub {
    position: sticky;
    top: var(--bar, 0px);
    z-index: 1;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: 0 0 14px;
    padding: 8px 0;
    background: var(--paper);
  }
  @media (max-width: 767px) {
    .scrub {
      top: 48px; /* under the top bar */
    }
  }
  .scrub input {
    flex: 1 1 200px;
    margin: 0;
  }
  .scrub button {
    width: 32px;
    padding: 0;
  }
  .where {
    min-width: 13ch;
    font-variant-numeric: tabular-nums;
  }
  .gallery {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(50% - 7px, 240px), 1fr)); /* two across on a phone */
    gap: 16px 14px;
    margin-bottom: 28px;
  }
  .key {
    font-family: var(--mono);
  }
  .cap {
    display: block;
  }
  figure {
    margin: 0;
  }
  img,
  video {
    width: 100%;
    display: block;
    border-radius: 6px;
    background: var(--panel);
  }
  :global(img.crisp) {
    image-rendering: pixelated;
  }
  .thumb {
    display: block;
    width: 100%;
    padding: 0;
    border: 0;
    background: none;
    cursor: zoom-in;
  }
  dialog {
    width: calc(100vw - 32px);
    height: calc(100dvh - 32px);
    max-width: none;
    max-height: none;
    padding: 0;
    border: 1px solid var(--rule);
    border-radius: var(--radius);
    background: var(--panel);
    color: var(--ink);
    outline: none;
  }
  dialog[open] {
    display: flex;
    flex-direction: column;
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 0.6);
  }
  /* The image takes what the info row leaves, scaled to fit, small samples up too. */
  dialog img {
    flex: 1;
    min-height: 0;
    object-fit: contain;
    border-radius: 0;
    background: var(--paper);
  }
  .info {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: center;
    gap: 8px 16px;
    padding: 10px 14px;
    border-top: 1px solid var(--rule);
  }
  .info p {
    margin: 2px 0 0;
  }
  .nav {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  figcaption {
    margin-top: 4px;
  }
</style>
