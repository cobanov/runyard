<script>
  import { store, ago, live } from './lib.svelte.js'
</script>

<div class="page-head"><h1>Projects</h1></div>

{#if store.projects?.length === 0}
  <div class="empty">
    <p>No runs yet. Start one from a training script:</p>
    <pre>pip install runyard
export RUNYARD_SERVER={location.origin} RUNYARD_API_KEY=…

import runyard as ry
run = ry.init(project="my-project", config={"{"}"lr": 1e-3{"}"})
run.log({"{"}"loss": loss{"}"}, step=step)</pre>
  </div>
{:else if store.projects}
  <div class="card table">
    <table>
      <thead><tr><th>Project</th><th>Runs</th><th>Running</th><th>Last started</th></tr></thead>
      <tbody>
        {#each store.projects as p (p.name)}
          <tr>
            <td class="name"><a href="#/p/{encodeURIComponent(p.name)}">{p.name}</a></td>
            <td>{p.runs}</td>
            <td>{#if p.running}<span class="badge status-running">{p.running}</span>{:else}<span class="muted">–</span>{/if}</td>
            <td class="muted">{p.last_run_at ? ago(p.last_run_at, live.now) : '–'}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .name {
    font-weight: 600;
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .name a {
    text-decoration: none;
  }
  .name a:hover {
    text-decoration: underline;
  }
</style>
