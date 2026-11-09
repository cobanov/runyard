<script>
  import { get, onEvent, throttle } from './lib.svelte.js'

  let { id, running } = $props()
  let lines = $state([])
  let loaded = $state(false)
  let box = $state()
  let follow = true // stick to the bottom unless the reader scrolled up
  let find = $state('')
  let top = $state(false) // the first line is loaded
  const shown = $derived(find.trim() ? lines.filter((l) => l.line.toLowerCase().includes(find.trim().toLowerCase())) : lines)
  // tqdm and logging write to stderr, so the stream says nothing; the failed colour is for lines that name a failure.
  // A traceback is red as a whole, from its first line to the exception that ends it, not only where a word matches.
  const failing = $derived.by(() => {
    const out = new Set()
    let trace = false
    for (const l of lines) {
      if (l.stream !== 2) continue
      if (/^Traceback/.test(l.line)) trace = true
      if (trace || /error|exception|nan\b/i.test(l.line)) out.add(l.seq)
      if (trace && !/^(Traceback|\s)/.test(l.line)) trace = false // the unindented line that names the exception
    }
    return out
  })
  const bad = (l) => failing.has(l.seq)
  async function earlier() {
    const before = lines[0].seq
    const prev = await get(`/runs/${id}/logs?tail&before=${before}&limit=2000`)
    top = prev.length < 2000
    follow = false
    lines = [...prev, ...lines]
  }

  async function more() {
    const after = lines.at(-1)?.seq ?? 0
    const next = await get(after ? `/runs/${id}/logs?after=${after}&limit=2000` : `/runs/${id}/logs?tail&limit=1000`)
    loaded = true
    if (!next.length) return
    if (!after) top = next.length < 1000
    lines = [...lines, ...next].slice(-20000)
    if (follow) requestAnimationFrame(() => box && (box.scrollTop = box.scrollHeight))
  }
  const load = throttle(more, 1500)
  load()
  $effect(() => onEvent((e) => (e.type === 'reconnect' || (e.run === id && e.type === 'logs')) && load()))
</script>

{#if loaded && !lines.length}
  <p class="empty">No output yet. Whatever the script prints shows up here{running ? ' as it happens' : ''}.</p>
{:else}
  <div class="tools">
    <input class="field" type="search" bind:value={find} placeholder="Find in the output" aria-label="Show only lines containing" />
    {#if loaded && !top}<button onclick={earlier}>Earlier lines</button>{/if}
    {#if find.trim()}<span class="muted small">{shown.length} of {lines.length} lines</span>{/if}
  </div>
  <pre bind:this={box} onscroll={() => (follow = box.scrollTop + box.clientHeight >= box.scrollHeight - 8)}>{#each shown as l (l.seq)}<span class:err={bad(l)} title={new Date(l.ts).toLocaleString()}>{l.line}
</span>{/each}</pre>
{/if}

<style>
  .tools {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
  }
  .tools .field {
    flex: 1 1 200px;
  }
  pre {
    margin: 0;
    background: var(--panel);
    border: 1px solid var(--rule);
    border-radius: var(--radius);
    padding: 10px 12px;
    height: min(70vh, 720px);
    overflow: auto;
    font-size: 0.8125rem;
    line-height: 1.5;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .err {
    color: var(--failed);
  }
</style>
