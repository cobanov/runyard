<script>
  import { get, send, onEvent, throttle, ago, live } from './lib.svelte.js'

  let alerts = $state(null)
  let error = $state('')
  const load = throttle(() => get('/alerts?limit=100').then((a) => (alerts = a)))
  load()
  $effect(() => onEvent((e) => (e.type === 'alert' || e.type === 'reconnect') && load()))
  // Dismissed alerts leave the Overview and stay here, dimmed. ids empty: all of them.
  const dismiss = (ids) =>
    send('POST', '/alerts/dismiss', { ids }).then(load, (e) => (error = e.message))
  const open = $derived(alerts?.filter((a) => !a.dismissed_at).length || 0)
</script>

<div class="page-head">
  <h1>Alerts</h1>
  {#if alerts}<span class="muted small">last {alerts.length}{open ? `, ${open} open` : ''}</span>{/if}
  {#if open}<button class="push" onclick={() => dismiss([])}>Dismiss all</button>{/if}
</div>
{#if error}<p class="empty"><span class="danger">Could not dismiss:</span> {error}</p>{/if}

{#if alerts?.length === 0}
  <p class="empty">No alerts yet. Runs raise them when they stall, fail or call <code>run.alert()</code>.</p>
{:else if alerts}
  <ul class="card row-list">
    {#each alerts as a (a.id)}
      <li class="level-{a.level}" class:dismissed={a.dismissed_at}>
        <div class="head">
          <strong>{a.title}</strong>
          <span class="when">
            <span class="muted small">{ago(a.ts, live.now)}</span>
            {#if !a.dismissed_at}<button class="small" onclick={() => dismiss([a.id])}>Dismiss</button>{/if}
          </span>
        </div>
        <p class="small muted"><a href="#/r/{a.run}">{a.run_name}</a> in <a href="#/p/{encodeURIComponent(a.project)}">{a.project}</a></p>
        {#if a.body}<pre class="small">{a.body}</pre>{/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  li {
    padding: 12px 16px;
    border-left: 3px solid var(--muted);
  }
  li:first-child {
    border-top-left-radius: var(--radius);
  }
  li:last-child {
    border-bottom-left-radius: var(--radius);
  }
  .level-warn {
    border-left-color: var(--crashed);
  }
  .level-error {
    border-left-color: var(--failed);
  }
  .dismissed {
    border-left-color: var(--rule);
    opacity: 0.6;
  }
  .push {
    margin-left: auto;
  }
  .when {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: none;
  }
  .head {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    overflow-wrap: anywhere;
  }
  p {
    margin: 2px 0 0;
  }
  pre {
    margin: 8px 0 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--muted);
  }
</style>
