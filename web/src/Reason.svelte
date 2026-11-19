<script>
  // Why a run that did not finish ended, in one line: the error's name in the state colour, the rest in ink.
  // In a list it is cut to the width it has, with the whole line on hover; on the run page it wraps.
  import { endReason } from './lib.svelte.js'
  let { run, wrap = false } = $props()
  const text = $derived(endReason(run))
  const cut = $derived(text.indexOf(': '))
</script>

{#if text}
  <span class="reason small" class:wrap title={wrap ? null : text}
    >{#if cut > 0}<span class="state">{text.slice(0, cut)}</span>{text.slice(cut)}{:else}<span class="state">{text}</span>{/if}</span
  >
{/if}

<style>
  .reason {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 400;
    color: var(--ink);
  }
  .wrap {
    white-space: normal;
    overflow-wrap: anywhere;
  }
</style>
