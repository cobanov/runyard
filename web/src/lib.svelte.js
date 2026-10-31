// Shared helpers: API calls, the live event stream, formatting.

export const get = (path) => send('GET', path)

// send calls the API; a failure throws an Error carrying the HTTP status.
export async function send(method, path, body) {
  const init = body ? { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : { method }
  const r = await fetch('/api/v1' + path, init)
  const out = await r.json().catch(() => ({}))
  if (!r.ok) throw Object.assign(new Error(out.error || r.statusText), { status: r.status })
  return out
}

// why puts a failed call in a sentence: 401 and 403 from a change mean it needs an admin.
export const why = (e) => (e.status === 401 || e.status === 403 ? 'Only an admin can do this: log in as one (the server needs RUNYARD_AUTH on).' : e.message)

export const live = $state({ connected: false, now: Date.now() })
setInterval(() => (live.now = Date.now()), 10000)

const listeners = new Set()
let source

// onEvent calls fn for every server event ({type, run}) and with {type: 'reconnect'} after the stream comes back.
export function onEvent(fn) {
  if (!source) {
    source = new EventSource('/api/v1/events')
    source.onopen = () => {
      live.connected = true
      for (const l of listeners) l({ type: 'reconnect' })
    }
    source.onerror = () => (live.connected = false) // EventSource retries on its own
    source.onmessage = (m) => {
      const e = JSON.parse(m.data)
      for (const l of listeners) l(e)
    }
  }
  listeners.add(fn)
  return () => listeners.delete(fn)
}

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
