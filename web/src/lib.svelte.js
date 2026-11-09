// Shared helpers: API calls, the live event stream, formatting.

export const get = (path) => send('GET', path)

// cred.key is an API key typed into Settings on a server without users (RUNYARD_AUTH off), where keys, channels
// and deletes need one; it goes with every call from this tab, and is forgotten when the tab closes.
export const cred = $state({ key: (() => { try { return sessionStorage.getItem('runyard.key') || '' } catch { return '' } })() })
export function setKey(key) {
  cred.key = key
  try {
    key ? sessionStorage.setItem('runyard.key', key) : sessionStorage.removeItem('runyard.key')
  } catch {}
}

// send calls the API; a failure throws an Error carrying the HTTP status.
export async function send(method, path, body) {
  const headers = cred.key ? { Authorization: 'Bearer ' + cred.key } : {}
  if (body) headers['Content-Type'] = 'application/json'
  let r
  try {
    r = await fetch('/api/v1' + path, { method, headers, body: body && JSON.stringify(body) })
  } catch (e) {
    live.down = 'no answer' // every page says so in one place (App), and keeps what it last showed
    throw e
  }
  live.down = r.status >= 500 ? `the server answered ${r.status}` : ''
  if (r.status === 401 && method === 'GET') live.expired = true // the session ran out: App shows the login form
  const out = await r.json().catch(() => ({}))
  if (!r.ok) throw Object.assign(new Error(out.error || r.statusText), { status: r.status })
  return out
}

// why puts a failed call in a sentence: 401 and 403 from a change mean it needs an admin.
export const why = (e) =>
  e.status === 401 || e.status === 403 ? 'Only an admin can do this: add an API key on the Settings page, or log in as an admin when the server has users.' : e.message

export const live = $state({ connected: false, now: Date.now(), down: '', expired: false, stale: false })
setInterval(() => (live.now = Date.now()), 10000)

const listeners = new Set()
let source
let missed = false // events arrived while the tab was hidden

function connect() {
  source = new EventSource('/api/v1/events')
  source.onopen = () => {
    live.connected = true
    emit({ type: 'reconnect' })
  }
  source.onerror = () => {
    live.connected = false
    // The browser retries a dropped connection on its own, but gives up for good on an HTTP error: a proxy's 502
    // while the server restarts, a session that ran out. Then the stream is made again.
    if (source.readyState === EventSource.CLOSED) setTimeout(connect, 5000)
  }
  source.onmessage = (m) => emit(JSON.parse(m.data))
}
// A tab nobody is looking at asks the server nothing; it catches up when it is looked at again.
function emit(e) {
  if (document.hidden) return void (missed = true)
  for (const l of listeners) l(e)
}
document.addEventListener('visibilitychange', () => {
  if (document.hidden || !missed) return
  missed = false
  emit({ type: 'reconnect' })
})

// onEvent calls fn for every server event ({type, run}) and with {type: 'reconnect'} after the stream comes back.
export function onEvent(fn) {
  if (!source) connect()
  listeners.add(fn)
  return () => listeners.delete(fn)
}

// title names the browser tab, so ten open runs can be told apart.
export const title = (t) => (document.title = t ? `${t} · runyard` : 'runyard')

// stuck is for how long a running run has logged nothing, no metric and no output line, once that is over ten
// minutes; 0 otherwise. Its heartbeat may be fine: the process is alive and the loop is not moving.
export const stuck = (run, now) => {
  const quiet = run.status === 'running' && run.progress_at ? now - new Date(run.progress_at) : 0
  return quiet > 600000 ? quiet : 0
}

// Chart view, shared by the run page and the comparison: smoothing and the log axis are remembered per browser.
// Storage may be blocked, so every access is guarded.
export const chartView = $state({ smooth: 0, log: false, time: false }) // time: x is time since the run started, not steps
try {
  const s = JSON.parse(localStorage.getItem('runyard.charts'))
  chartView.smooth = Math.min(0.99, Math.max(0, +s.smooth || 0))
  chartView.log = s.log === true
  chartView.time = s.time === true
} catch {}

// seriesLine turns one metric's series into a chart line; with time on (and a server that sends it) x is seconds
// since the run started.
export function seriesLine(s, run, time) {
  const t0 = new Date(run?.started_at) / 1000
  return { x: time && s.t && t0 ? s.t.map((t) => Math.max(0, t - t0)) : s.step, y: s.last.map(plotValue) }
}

// thumb is a sample's URL at a width fit for a grid; the server keeps 320, 640 and 1280. A GIF stays itself: it moves.
export const thumb = (url, w = 640) => (url.endsWith('.gif') ? url : `${url}?w=${w}`)

// memo keeps what a page was showing (a list's filter and search, a run's tab) for when you come Back to it.
export const memo = {}

// After the server was upgraded, an open tab still runs the old page: its script is no longer the one index.html names.
export async function checkStale() {
  const mine = document.querySelector('script[type=module][src]')?.getAttribute('src')
  if (!mine || !mine.includes('/assets/')) return // the dev server
  try {
    const page = await (await fetch('/', { cache: 'no-store' })).text()
    live.stale = !page.includes(mine)
  } catch {}
}
$effect.root(() => {
  $effect(() => {
    const v = JSON.stringify(chartView)
    try {
      localStorage.setItem('runyard.charts', v)
    } catch {}
  })
})

// throttle runs fn at most once per ms, always ending with a call after the last request.
export function throttle(fn, ms = 2000) {
  let last = 0
  let timer
  return () => {
    const wait = last + ms - Date.now()
    if (wait <= 0) {
      last = Date.now()
      fn()
    } else if (!timer) {
      timer = setTimeout(() => {
        timer = null
        last = Date.now()
        fn()
      }, wait)
    }
  }
}

// Metric values arrive as numbers, or as "NaN"/"Infinity" strings; charts get null for those.
export const plotValue = (v) => (typeof v === 'number' ? v : null)

export function fmt(v) {
  if (typeof v !== 'number') return String(v)
  if (Number.isInteger(v)) return v.toLocaleString('en-US')
  const a = Math.abs(v)
  return a !== 0 && (a < 1e-3 || a >= 1e5) ? v.toExponential(2) : String(+v.toPrecision(4))
}

export function duration(ms) {
  const s = Math.max(0, Math.round(ms / 1000))
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h ${m % 60}m`
  return `${Math.floor(h / 24)}d ${h % 24}h`
}

export function ago(t, now) {
  return duration(now - new Date(t)) + ' ago'
}

// progressText says how far a run is, when its config names a total: steps (max_steps and the like) against the
// latest logged step, else epochs against an epoch metric. Nested config keys count by their last part. As in
// ios/Runyard/API.swift.
export function progressText(run, now) {
  const cfg = {}
  for (const [k, v] of Object.entries(flatten(run.config))) {
    const name = k.split('.').pop()
    if (typeof v === 'number') cfg[name] = Math.max(cfg[name] ?? v, v) // sampler.steps must not hide train.steps
  }
  const total = (...names) => names.map((n) => cfg[n]).find((t) => t > 0)
  const train = Object.entries(run.metrics || {}).filter(([k]) => !k.startsWith('sys/'))
  const step = Math.max(0, ...train.map(([, m]) => m.step))
  const steps = total('max_steps', 'total_steps', 'num_steps', 'train_steps', 'steps')
  const epochs = total('num_train_epochs', 'epochs', 'max_epochs', 'num_epochs')
  const epoch = train.find(([k]) => k === 'epoch' || k.endsWith('/epoch'))?.[1].last
  // A plain "steps" may be a sampler's, so one the run has passed is not its total.
  const [done, all, unit] = steps && step <= steps ? [step, steps, 'step'] : [epoch, epochs, 'epoch']
  if (!all || !Number.isFinite(done)) return ''
  const f = Math.min(Math.max(done / all, 0), 1)
  const elapsed = now - new Date(run.started_at)
  // Elapsed time includes setup, so this is a rough guess.
  const left = run.status === 'running' && f > 0.01 && f < 1 ? ` · ~${duration((elapsed * (1 - f)) / f)} left` : ''
  return `${unit} ${fmt(done)} / ${fmt(all)} · ${Math.round(f * 100)}%${left}`
}

export function runTime(run, now) {
  return duration((run.ended_at ? new Date(run.ended_at) : now) - new Date(run.started_at))
}

// flatten turns nested config into dotted keys, so configs can be listed and compared line by line.
export function flatten(obj, prefix = '', out = {}) {
  for (const [k, v] of Object.entries(obj || {})) {
    if (v && typeof v === 'object' && !Array.isArray(v)) flatten(v, prefix + k + '.', out)
    else out[prefix + k] = Array.isArray(v) ? JSON.stringify(v) : v
  }
  return out
}

// Projects, shared by the sidebar and the Projects page; reloaded on every run event.
export const store = $state({ projects: null })
export const loadProjects = throttle(() => get('/projects').then((p) => void (store.projects = p)).catch(() => {}))

// progressOf is how far a run is, 0 to 1, or null when its config names no total: what progressText says, as a bar.
export const progressOf = (run, now) => {
  const pct = progressText(run, now).match(/(\d+)%/)
  return pct ? pct[1] / 100 : null
}

// byInterest orders metric names for reading: losses first (the shortest name leads), then the rest by name.
export const byInterest = (a, b) => /loss/i.test(b) - /loss/i.test(a) || (/loss/i.test(a) ? a.length - b.length : 0) || a.localeCompare(b)

// mainMetric picks the key to show for a run at a glance: a loss if there is one, else the most logged non-sys key.
export function mainMetric(metrics) {
  const keys = Object.keys(metrics || {}).filter((k) => !k.startsWith('sys/'))
  const loss = keys.filter((k) => /loss/i.test(k)).sort((a, b) => a.length - b.length)[0]
  return loss || keys.sort((a, b) => metrics[b].count - metrics[a].count)[0]
}

// sysLoad groups the last sys/* values of running runs by host: {host: {cpu, ram_gb, gpus: [{i, util, mem_gb, temp_c, power_w}]}}
export function sysLoad(runs) {
  const hosts = {}
  for (const r of runs) {
    for (const [k, m] of Object.entries(r.metrics || {})) {
      if (!k.startsWith('sys/')) continue
      const h = (hosts[r.host || '?'] ||= { gpus: {} })
      const gpu = k.match(/^sys\/gpu(\d+)\.(\w+)$/)
      if (gpu) (h.gpus[gpu[1]] ||= { i: +gpu[1] })[gpu[2]] = m.last
      else h[k.slice(4)] = m.last
    }
  }
  return Object.entries(hosts).map(([host, h]) => ({ host, ...h, gpus: Object.values(h.gpus).sort((a, b) => a.i - b.i) }))
}

export const palette = ['--c1', '--c2', '--c3', '--c4', '--c5', '--c6']
export const cssVar = (name) => getComputedStyle(document.documentElement).getPropertyValue(name).trim()

// cssColor resolves a colour token to rgb(): the raw custom property still holds light-dark(...), which canvas cannot draw.
const probe = document.createElement('i')
probe.style.display = 'none'
document.body.append(probe)
export function cssColor(name) {
  probe.style.color = `var(${name})`
  return getComputedStyle(probe).color
}
export function withAlpha(rgb, a) {
  const [r, g, b] = rgb.match(/[\d.]+/g)
  return `rgba(${r}, ${g}, ${b}, ${a})`
}

// theme.dark flips with the OS setting or a data-theme attribute on <html>; charts redraw on it.
const dark = matchMedia('(prefers-color-scheme: dark)')
const isDark = () => (document.documentElement.dataset.theme || (dark.matches ? 'dark' : 'light')) === 'dark'
export const theme = $state({ dark: isDark() })
dark.addEventListener('change', () => (theme.dark = isDark()))
new MutationObserver(() => (theme.dark = isDark())).observe(document.documentElement, { attributeFilter: ['data-theme'] })
