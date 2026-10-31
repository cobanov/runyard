<script>
  import { get, onEvent, throttle } from './lib.svelte.js'
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

  function stepOf(key) {
    const g = groups[key]
    return g.steps[pick[key] ?? g.steps.length - 1]
  }

  // Small images (pixel art, tiny diffusion samples) stay crisp when scaled up.
  const crisp = (e) => e.target.naturalWidth < 256 && e.target.classList.add('crisp')
</script>

{#if media?.length === 0}
  <p class="empty">No samples yet. Log them with <code>run.image()</code>, <code>run.video()</code> or <code>run.text()</code>.</p>
{/if}
{#each Object.entries(groups) as [key, g] (key)}
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
    {#if g.kind === 'text'}
      {#each g.at[step] as m (m.id)}
        <TextTable url={m.url} />
      {/each}
    {:else}
      <div class="gallery">
        {#each g.at[step] as m (m.id)}
          <figure>
            {#if m.kind === 'video' && !m.url.endsWith('.gif')}
              <video src={m.url} controls loop muted playsinline></video>
            {:else}
              <a href={m.url} target="_blank"><img src={m.url} alt={m.meta.caption || key} loading="lazy" onload={crisp} /></a>
            {/if}
            {#if m.meta.caption}<figcaption class="small muted">{m.meta.caption}</figcaption>{/if}
          </figure>
        {/each}
      </div>
    {/if}
  </section>
{/each}

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
  .gallery {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 260px), 1fr));
    gap: 12px;
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
  figcaption {
    margin-top: 4px;
  }
</style>
