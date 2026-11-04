<script>
  // The kiosk: one card per coding agent session, for a TV, a monitor or a phone left on a stand. No menu and no
  // buttons; everything is in the URL: #/kiosk?project=<name>&hours=24&theme=light. Whatever needs a person sorts first.
  import { onDestroy } from 'svelte'
  import { get, onEvent, throttle, live, ago, fmt, plotValue, mainMetric } from './lib.svelte.js'
  import Sparkline from './Sparkline.svelte'

  const q = new URLSearchParams(location.hash.split('?')[1] || '')
  const project = q.get('project') || ''
  const hours = q.get('hours') || '24'

  let agents = $state(null)
  let runs = $state({}) // agent session id -> its runs, oldest first, with metric summaries
  let loadedAt = $state(Date.now())
  let error = $state('')

  const order = { waiting: 0, error: 1, done: 2, training: 3, working: 3, idle: 4, stale: 5, ended: 6 }
  const words = { waiting: 'Waiting for you', error: 'Error', done: 'Done', training: 'Training', working: 'Working', idle: 'Idle', stale: 'Quiet', ended: 'Ended' }

  const loadAgents = throttle(async () => {
    try {
      const list = await get(`/agents?hours=${encodeURIComponent(hours)}&project=${encodeURIComponent(project)}`)
      agents = list.sort((a, b) => order[a.state] - order[b.state] || new Date(b.active_at) - new Date(a.active_at))
      loadedAt = Date.now()
      error = ''
      loadRuns()
    } catch (e) {
      error = e.message
    }
  }, 3000)
  // shortcut: the 200 newest runs of each project shown; an agent with older runs loses them from its trend line
  const loadRuns = throttle(async () => {
    const projects = [...new Set((agents || []).map((a) => a.project).filter(Boolean))]
    const lists = await Promise.all(projects.map((p) => get(`/runs?project=${encodeURIComponent(p)}&metrics=1&limit=200`).catch(() => [])))
    const by = {}
    for (const r of lists.flat().sort((a, b) => new Date(a.started_at) - new Date(b.started_at))) if (r.agent) (by[r.agent] ||= []).push(r)
    runs = by
  }, 10000)
  const load = loadAgents

  // The card's score: the project's main metric over the agent's runs (a loss when the project names none).
  function score(a) {
    const rs = runs[a.id] || []
    const key = a.metric || mainMetric([...rs].reverse().find((r) => mainMetric(r.metrics))?.metrics)
    if (!key) return null
    const vals = rs.map((r) => plotValue(r.metrics?.[key]?.last) ?? null)
    const got = vals.filter((v) => v != null)
    if (!got.length) return null
    const goal = a.metric ? a.goal : 'min'
    return { key, goal, last: got.at(-1), best: goal === 'max' ? Math.max(...got) : Math.min(...got), vals }
  }

  const title = (a) => a.label || a.cwd.split('/').filter(Boolean).at(-1) || a.tool

  // Stay awake and stay current: the screen lock, a full reload every minute (a phone drops the event stream
  // without saying so) and on coming back to the page, and a 1 px shift every few minutes against burn-in.
  let lock
  async function wake() {
    try {
      if (document.visibilityState === 'visible') lock = await navigator.wakeLock?.request('screen')
    } catch {}
  }
  function visible() {
    if (document.visibilityState !== 'visible') return
    wake()
    load()
  }
  let shift = $state(0)
  const timers = [setInterval(load, 60000), setInterval(() => (shift = (shift + 1) % 4), 180000)]
  document.addEventListener('visibilitychange', visible)
  const theme = document.documentElement.dataset.theme
  document.documentElement.dataset.theme = q.get('theme') || 'dark'
  wake()
  load()
  const off = onEvent((e) => {
    if (e.type === 'agent' || e.type === 'run' || e.type === 'reconnect') loadAgents()
    if (e.type === 'run' || e.type === 'metrics') loadRuns()
  })
  onDestroy(() => {
    off()
    timers.forEach(clearInterval)
    document.removeEventListener('visibilitychange', visible)
    lock?.release().catch(() => {})
    if (theme) document.documentElement.dataset.theme = theme
    else delete document.documentElement.dataset.theme
  })

  const behind = $derived(live.now - loadedAt > 180000)
</script>

<div class="kiosk" class:behind style="translate: {[0, 1, 1, 0][shift]}px {[0, 0, 1, 1][shift]}px">
  <header>
    <a class="where" href="#/">runyard{project ? ` · ${project}` : ''}</a>
    <span class="muted" role="status">
      {#if behind}Not updated for {ago(loadedAt, live.now).replace(' ago', '')}{error ? `: ${error}` : ''}{:else}<span class="live" class:on={live.connected}></span>Live{/if}
    </span>
  </header>

  {#if agents?.length === 0}
    <p class="empty">No coding agent has started a run{project ? ` in ${project}` : ''} in the last {hours} hours. Runs started from Claude Code, Codex or OpenCode show up here on their own.</p>
  {:else if agents}
    <ul class="grid">
      {#each agents as a (a.id)}
        {@const s = score(a)}
        <li class="card st-{a.state}">
          <div class="top">
            <h2 class="name">{title(a)}</h2>
            {#key a.state}<span class="state">{words[a.state]}</span>{/key}
          </div>
          <p class="meta">{[a.tool, a.project, a.host].filter(Boolean).join(' · ')}</p>
          {#if s}
            <div class="score">
              <span class="big">{fmt(s.last)}</span>
              <span class="best" title="Best so far ({s.goal === 'max' ? 'highest' : 'lowest'})">★ {fmt(s.best)}</span>
              <span class="key">{s.key}</span>
            </div>
            {#if s.vals.length > 1}<Sparkline values={s.vals} color={a.state === 'error' ? '--failed' : '--running'} height={40} />{/if}
          {/if}
          {#if a.report || a.said}
            <p class="said">{a.report || a.said}</p>
          {/if}
          {#if a.report && a.next}<p class="next">Next: {a.next}</p>{/if}
          <p class="foot">
            {#if a.last_run}<span class="run">{a.last_run.name}</span> ·{/if}
            {a.runs === 1 ? '1 run' : `${a.runs} runs`}{a.kept ? ` · ${a.kept} kept` : ''} · {ago(a.active_at, live.now)}
          </p>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .kiosk {
    min-height: 100dvh;
    padding: clamp(12px, 2.5vmin, 32px);
    font-size: clamp(15px, 1.6vmin, 22px);
  }
  .behind {
    filter: grayscale(1);
    opacity: 0.6;
  }
  header {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: clamp(10px, 2vmin, 24px);
  }
  .where {
    font-weight: 600;
    color: inherit;
    text-decoration: none;
  }
  .live {
    display: inline-block;
    width: 0.5em;
    height: 0.5em;
    margin-right: 0.4em;
    border-radius: 50%;
    background: var(--crashed);
  }
  .live.on {
    background: var(--running);
  }
  .grid {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 22em), 1fr));
    gap: clamp(10px, 1.6vmin, 20px);
  }
  .card {
    --state: var(--muted);
    padding: 1em 1.1em;
    min-width: 0;
    border-left: 4px solid var(--state);
  }
  .st-waiting { --state: var(--crashed); }
  .st-error { --state: var(--failed); }
  .st-done { --state: var(--c6); }
  .st-training { --state: var(--running); }
  .st-working { --state: var(--c1); }
  .st-stale,
  .st-ended {
    opacity: 0.55;
  }
  .top {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
  }
  .name {
    margin: 0;
    font-size: 1.15em;
    font-weight: 600;
    color: var(--ink);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .state {
    display: inline-flex;
    align-items: center;
    gap: 0.4em;
    flex: none;
    color: var(--state);
    font-weight: 600;
    font-size: 0.9em;
  }
  .state::before {
    content: '';
    width: 0.6em;
    height: 0.6em;
    border-radius: 50%;
    background: var(--state);
  }
  /* A change to done or waiting pulses five times at 1 Hz, then stays still (WCAG 2.2.2, 2.3.1). */
  @media (prefers-reduced-motion: no-preference) {
    .st-done .state::before,
    .st-waiting .state::before {
      animation: pulse 1s ease-in-out 5;
    }
  }
  @keyframes pulse {
    50% {
      transform: scale(1.8);
      opacity: 0.4;
    }
  }
  p {
    margin: 0;
  }
  .meta,
  .foot,
  .next,
  .key {
    color: var(--muted);
    font-size: 0.85em;
  }
  .meta,
  .next,
  .foot {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .score {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 0 0.6em;
    margin-top: 0.6em;
  }
  .big {
    font-size: clamp(1.8em, 5vmin, 3.2em);
    font-weight: 600;
    line-height: 1.1;
  }
  .best {
    font-weight: 600;
  }
  .said {
    margin-top: 0.6em;
    display: -webkit-box;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    overflow: hidden;
  }
  .foot {
    margin-top: 0.6em;
  }
  .run {
    color: var(--ink);
  }
  .empty {
    font-size: 1.1em;
  }
</style>
