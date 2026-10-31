<script>
  import { get, onEvent, throttle } from './lib.svelte.js'

  let { id, running } = $props()
  let lines = $state([])
  let loaded = $state(false)
  let box = $state()
  let follow = true // stick to the bottom unless the reader scrolled up

  async function more() {
    const after = lines.at(-1)?.seq ?? 0
    const next = await get(after ? `/runs/${id}/logs?after=${after}&limit=2000` : `/runs/${id}/logs?tail&limit=1000`)
    loaded = true
    if (!next.length) return
    lines = [...lines, ...next].slice(-5000)
    if (follow) requestAnimationFrame(() => box && (box.scrollTop = box.scrollHeight))
  }
  const load = throttle(more, 1500)
  load()
  $effect(() => onEvent((e) => (e.type === 'reconnect' || (e.run === id && e.type === 'logs')) && load()))
</script>

{#if loaded && !lines.length}
  <p class="empty">No output yet. Whatever the script prints shows up here{running ? ' as it happens' : ''}.</p>
{:else}
  <pre bind:this={box} onscroll={() => (follow = box.scrollTop + box.clientHeight >= box.scrollHeight - 8)}>{#each lines as l (l.seq)}<span class:err={l.stream === 2}>{l.line}
</span>{/each}</pre>
{/if}

<style>
  pre {
    margin: 0;
    background: var(--panel);
    border: 1px solid var(--rule);
    border-radius: 8px;
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
