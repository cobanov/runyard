<script>
  import { get, send, onEvent, throttle, ago, duration, live } from './lib.svelte.js'

  let alerts = $state(null)
  let error = $state('')
  let unsent = $state([]) // notifications a channel did not take: still being tried, or given up on
  const load = throttle(() => {
    get('/alerts?limit=100').then((a) => (alerts = a))
    get('/notifications/undelivered').then((n) => (unsent = n), () => {}) // an older server has no outbox
  })
  load()
  $effect(() => onEvent((e) => (e.type === 'alert' || e.type === 'reconnect') && load()))
  // Dismissed alerts leave the Overview and stay here, dimmed. ids empty: all of them.
  const dismiss = (ids) =>
    send('POST', '/alerts/dismiss', { ids }).then(load, (e) => (error = e.message))
  const putAway = (ids) =>
    send('POST', '/notifications/dismiss', { ids }).then(load, (e) => (error = e.message))
  const open = $derived(alerts?.filter((a) => !a.dismissed_at).length || 0)
  const channel = (n) => `${n.kind} channel ${n.channel}`
</script>

<div class="page-head">
  <h1>Alerts</h1>
  {#if alerts}<span class="muted small">last {alerts.length}{open ? `, ${open} open` : ''}</span>{/if}
  {#if open}<button class="push" onclick={() => dismiss([])}>Dismiss all</button>{/if}
</div>
{#if error}<p class="empty"><span class="danger">Could not dismiss:</span> {error}</p>{/if}

{#if unsent.length}
  <h2>Notifications not sent</h2>
  <ul class="card row-list unsent">
    {#each unsent as n (n.id)}
      <li class={n.next_at ? 'level-warn' : 'level-error'}>
        <div class="head">
          <strong>{n.title}</strong>
          <span class="when">
            <span class="muted small">{ago(n.created_at, live.now)}</span>
            {#if !n.next_at}<button class="small" onclick={() => putAway([n.id])}>Dismiss</button>{/if}
          </span>
        </div>
        <p class="small muted">
          {#if n.next_at}
            Trying {channel(n)} again {new Date(n.next_at) > live.now ? `in ${duration(new Date(n.next_at) - live.now)}` : 'now'}, after {n.tries} failed {n.tries === 1 ? 'try' : 'tries'}
          {:else}
            <span class="danger">Not delivered</span> to {channel(n)} after {n.tries} {n.tries === 1 ? 'try' : 'tries'}
          {/if}
          {#if n.run}· <a href="#/r/{n.run}">the run</a>{/if}
        </p>
        {#if n.error}<pre class="small">{n.error}</pre>{/if}
      </li>
    {/each}
  </ul>
  {#if alerts?.length}<h2>Alerts</h2>{/if}
{/if}

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
  .unsent {
    margin-bottom: 24px;
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
