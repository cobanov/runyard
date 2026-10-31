<script>
  import { get, onEvent, throttle, ago, live } from './lib.svelte.js'

  let alerts = $state(null)
  const load = throttle(() => get('/alerts?limit=100').then((a) => (alerts = a)))
  load()
  $effect(() => onEvent((e) => (e.type === 'alert' || e.type === 'reconnect') && load()))
</script>

<div class="page-head">
  <h1>Alerts</h1>
  {#if alerts}<span class="muted small">last {alerts.length}</span>{/if}
</div>

{#if alerts?.length === 0}
  <p class="empty">No alerts yet. Runs raise them when they stall, fail or call <code>run.alert()</code>.</p>
{:else if alerts}
  <ul class="card row-list">
    {#each alerts as a (a.id)}
      <li class="level-{a.level}">
        <div class="head">
          <strong>{a.title}</strong>
          <span class="muted small">{ago(a.ts, live.now)}</span>
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
