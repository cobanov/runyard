<script>
  import { get, onEvent, throttle, ago, live } from './lib.svelte.js'

  let projects = $state(null)
  let error = $state('')
  const load = throttle(() => get('/projects').then((p) => ((projects = p), (error = ''))).catch((e) => (error = e.message)))
  load()
  $effect(() => onEvent((e) => (e.type === 'run' || e.type === 'reconnect') && load()))
</script>

<h1>Projects</h1>
{#if error}
  <p class="empty">Could not load projects: {error}</p>
{:else if projects?.length === 0}
  <div class="empty">
    <p>No runs yet. Start one from a training script:</p>
    <pre>pip install runyard
export RUNYARD_SERVER={location.origin} RUNYARD_API_KEY=…

import runyard as ry
run = ry.init(project="my-project", config={"{"}"lr": 1e-3{"}"})
run.log({"{"}"loss": loss{"}"}, step=step)</pre>
  </div>
{:else if projects}
  <ul class="row-list">
    {#each projects as p (p.name)}
      <li>
        <a href="#/p/{encodeURIComponent(p.name)}" class="edge" class:status-running={p.running > 0}>
          <span class="name">{p.name}</span>
          <span class="muted small">
            {#if p.running}<span class="state status-running">{p.running} running</span>, {/if}{p.runs} runs{#if p.last_run_at}, last started {ago(p.last_run_at, live.now)}{/if}
          </span>
        </a>
      </li>
    {/each}
  </ul>
{/if}

<style>
  h1 {
    margin: 8px 0 16px;
  }
  a {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    align-items: baseline;
    gap: 4px 16px;
    padding: 14px 12px;
    border-bottom: 1px solid var(--rule);
    text-decoration: none;
  }
  a:hover {
    background: var(--panel);
  }
  .name {
    font-weight: 700;
    font-size: 1.125rem;
  }
</style>
