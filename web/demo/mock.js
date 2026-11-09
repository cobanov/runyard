// The demo's server: runyard's REST API and live event stream, answered inside the browser from made-up runs that
// keep training while the page is open. Nothing leaves the page, and a reload starts over.

const T0 = Date.now()
const S = 1000
const M = 60 * S
const H = 60 * M
const D = 24 * H
const iso = (t) => new Date(t).toISOString()
const p5 = (v) => +v.toPrecision(5)

// Seeded noise: the history looks the same on every visit, and live steps come from the same functions.
function hash(n) {
  n = Math.imul((n | 0) ^ 0x9e3779b9, 0x85ebca6b)
  n = Math.imul(n ^ (n >>> 13), 0xc2b2ae35)
  return ((n ^ (n >>> 16)) >>> 0) / 4294967296
}
const u = (seed, i) => hash(Math.imul(seed, 0x27d4eb2f) + (i | 0)) // 0..1
const g = (seed, i) => (u(seed, i) + u(seed + 1, i) + u(seed + 2, i) - 1.5) * 2 // about normal
const within = ([lo, hi], seed, i) => lo + (hi - lo) * u(seed, i)
const hex = (seed, n) => Array.from({ length: n }, (_, i) => Math.floor(u(seed, i) * 16).toString(16)).join('')
const uuid = (seed) => {
  const h = hex(seed, 32)
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-4${h.slice(13, 16)}-a${h.slice(17, 20)}-${h.slice(20)}`
}

// ---- the data ----

const projects = new Map() // name -> {name, created_at}
const runs = new Map() // id -> run (the API fields plus the simulation's own)
const alerts = []
const keys = [
  { id: 1, name: 'a100-node', user: '', created_at: iso(T0 - 41 * D) },
  { id: 2, name: 'lab-4090', user: '', created_at: iso(T0 - 33 * D) },
  { id: 3, name: 'claude-code', user: '', created_at: iso(T0 - 6 * D) },
]
const channels = [
  { id: 1, kind: 'ntfy', target: 'https://ntfy.sh/my-lab-runs', project: '', events: ['finished', 'failed', 'crashed', 'alert'] },
  { id: 2, kind: 'slack', target: 'https://hooks.slack.com/services/T000/B000/demo', project: 'qwen-sft', events: ['failed', 'crashed'] },
]
let nextId = 100
let booting = true

// ---- the event stream ----

const sources = new Set()
const publish = (e) => !booting && sources.forEach((s) => s.onmessage?.({ data: JSON.stringify(e) }))
class DemoEventSource {
  constructor() {
    sources.add(this)
    setTimeout(() => this.onopen?.({}), 60)
  }
  close() {
    sources.delete(this)
  }
  addEventListener() {}
}

// notices are what your phone would get: the demo shows them in the page.
const noticeFns = new Set()
export const onNotice = (fn) => (noticeFns.add(fn), () => noticeFns.delete(fn))
const notice = (n) => !booting && noticeFns.forEach((fn) => fn(n))

// ---- runs ----

function project(name, t) {
  if (!projects.has(name)) projects.set(name, { name, created_at: iso(t) })
}

// The coding agents behind the runs, by run seed; agentOut works out their cards from those runs, as the server does.
const agents = [
  { id: 'claude:7f3a', tool: 'claude', cwd: '/work/qwen-sft', project: 'qwen-sft', host: 'a100-node', metric: 'eval/loss', goal: 'min', seeds: [11, 12, 13],
    report: 'Rank 64 finished at 1.128 eval loss. Rank 16 without packing crashed at 1.222, so this run puts packing back.', next: 'If rank 16 stays above 1.15, back to rank 64' },
  { id: 'codex:c21e', tool: 'codex', cwd: '/work/pixel-diffusion', project: 'pixel-diffusion', host: 'lab-4090', metric: 'eval/fid', goal: 'min', seeds: [21, 22, 23],
    report: 'EMA 0.999 took FID from 26.4 with the U-Net to 20.3. Saving samples for the next run needs 40 GB of disk, so I am asking first.', next: 'Keep the DiT, try a longer EMA', waiting: true },
  { id: 'claude:b04d', tool: 'claude', cwd: '/work/cifar10', project: 'cifar10-resnet', host: 'lab-4090', metric: 'val/acc', goal: 'max', seeds: [31, 32],
    report: 'lr 0.1 reached 96.1% against 94.9% for 0.05, in the same 30 minutes. lr 0.1 stays and the sweep stops here.' },
]

// addRun starts a run at o.start; advance() then plays its steps up to any moment, history and live alike.
function addRun(o) {
  const r = {
    id: uuid(o.seed),
    agent: agents.find((a) => a.seeds.includes(o.seed))?.id ?? null,
    project: o.project,
    name: o.name,
    status: 'running',
    config: o.endStep ? { max_steps: o.endStep, ...o.config } : o.config, // a total, so cards and lists show progress
    summary: {},
    tags: o.tags || [],
    notes: o.notes || '',
    host: o.host,
    git: { branch: o.branch || 'main', commit: hex(o.seed + 7, 40), dirty: !!o.dirty },
    started_at: iso(o.start),
    ended_at: null,
    last_heartbeat_at: iso(o.start),
    // the simulation
    seed: o.seed,
    start: o.start,
    spp: o.secPerStep * S,
    model: o.model,
    gpus: o.gpus || [],
    prof: o.prof,
    proc: o.proc,
    user: o.user,
    pid: 10000 + Math.floor(u(o.seed, 99) * 50000),
    endStep: o.endStep ?? null,
    fate: o.fate || 'finished',
    nanAt: o.nanAt ?? null,
    step: 0,
    sysT: o.start,
    seq: 0,
    series: {},
    sums: {},
    lines: [],
    media: [],
    artifacts: [],
    dirty: new Set(),
  }
  project(r.project, o.start)
  runs.set(r.id, r)
  r.model.begin?.(r, o.start)
  return r
}

const timeAt = (r, s) => r.start + s * r.spp
const stepAt = (r, t) => Math.floor((t - r.start) / r.spp)
const endMs = (r) => (r.ended_at ? new Date(r.last_heartbeat_at).getTime() : Infinity)

function put(r, key, step, v) {
  ;(r.series[key] ||= []).push([step, v])
  const m = r.sums[key]
  const num = typeof v === 'number'
  if (!m) r.sums[key] = { count: 1, step, last: v, min: v, max: v }
  else {
    m.count++
    m.step = step
    m.last = v
    if (num) {
      m.min = typeof m.min === 'number' ? Math.min(m.min, v) : v
      m.max = typeof m.max === 'number' ? Math.max(m.max, v) : v
    }
  }
  r.dirty.add('metrics')
}
function line(r, t, text, stream = 1) {
  r.lines.push({ seq: ++r.seq, ts: iso(t), stream, line: text })
  if (r.lines.length > 3000) r.lines.splice(0, r.lines.length - 3000)
  r.dirty.add('logs')
}
let mediaId = 1
function media(r, key, step, kind, url, meta, t) {
  r.media.push({ id: String(mediaId++), key, step, kind, url, meta, created_at: iso(t) })
  r.dirty.add('media')
}
function artifact(r, name, kind, step, size, t) {
  r.artifacts.push({ id: uuid(r.seed * 31 + step), name, kind, step, uri: `${r.host}:/data/runs/${r.name}/${name}`, size, meta: {}, created_at: iso(t) })
  r.dirty.add('artifact')
}
let alertId = 1
function alert(r, t, level, title, body = '') {
  alerts.unshift({ id: alertId++, run: r.id, ts: iso(t), level, title, body, dismissed_at: null })
  publish({ type: 'alert', run: r.id })
  notice({ level, title: `${r.name}: ${title}`, body: body.split('\n')[0], run: r.id })
}

function end(r, status, t) {
  r.status = status
  r.last_heartbeat_at = iso(t)
  r.ended_at = iso(status === 'crashed' ? t + 5 * M : t) // crashed: noticed five minutes after the last heartbeat
  r.summary = Object.fromEntries(Object.entries(r.sums).filter(([k, m]) => !k.startsWith('sys/') && typeof m.last === 'number').map(([k, m]) => [k, m.last]))
  publish({ type: 'run', run: r.id })
  const took = dur(new Date(r.ended_at) - r.start)
  notice({ level: status === 'finished' ? 'info' : 'error', title: `${r.name} ${status}`, body: status === 'finished' ? `after ${took}` : status === 'crashed' ? 'No heartbeat for five minutes.' : 'The script exited with an error.', run: r.id })
}
const dur = (ms) => {
  const m = Math.round(ms / M)
  return m < 60 ? `${m}m` : `${Math.floor(m / 60)}h ${m % 60}m`
}

// advance plays a run's steps and system readings up to time t.
function advance(r, t) {
  if (r.status !== 'running') return
  const m = r.model
  const target = Math.min(stepAt(r, t), r.endStep ?? Infinity)
  while (r.step + m.every <= target) {
    r.step += m.every
    const s = r.step
    const ts = timeAt(r, s)
    while (r.sysT + 10 * S <= ts) sys(r, (r.sysT += 10 * S))
    m.at(r, s, ts)
    if (r.nanAt != null && s >= r.nanAt) {
      m.nan?.(r, s, ts)
      alert(r, ts, 'error', 'loss is NaN', `train/loss NaN at step ${s.toLocaleString('en-US')}: stopping.\nLower the learning rate or clip gradients.`)
      return end(r, 'failed', ts + 2 * S)
    }
    if (r.endStep != null && s >= r.endStep) {
      if (r.fate === 'finished') m.done?.(r, s, ts)
      return end(r, r.fate, ts)
    }
  }
  while (r.sysT + 10 * S <= t) sys(r, (r.sysT += 10 * S))
  r.last_heartbeat_at = iso(Math.min(t, Date.now()))
}

// sys logs what runyard's SDK samples every 10 s: CPU, memory, and each of the run's GPUs.
function sys(r, t) {
  const e = Math.round((t - r.start) / S)
  const i = Math.floor(t / (10 * S))
  const p = r.prof
  put(r, 'sys/cpu', e, p5(within(p.cpu, r.seed + 10, i)))
  put(r, 'sys/ram_gb', e, p5(p.ram + 0.4 * g(r.seed + 11, i)))
  for (const gi of r.gpus) {
    const v = gpuLoad(p, r.seed + gi, i)
    for (const k of ['util', 'mem_gb', 'temp_c', 'power_w']) put(r, `sys/gpu${gi}.${k}`, e, v[k])
  }
}
function gpuLoad(p, seed, i) {
  return {
    util: Math.round(Math.min(100, within(p.util, seed + 20, i))),
    mem_gb: p5(p.mem + 0.05 * g(seed + 21, i)),
    temp_c: Math.round(p.temp[0] + (p.temp[1] - p.temp[0]) * (0.5 + 0.35 * Math.sin(i / 40) + 0.15 * u(seed + 22, i))),
    power_w: Math.round(within(p.power, seed + 23, i)),
  }
}

// ---- models: what each kind of run logs at a step ----

const cosine = (s, total, peak, warm) => (s < warm ? (peak * s) / warm : (peak * 0.5 * (1 + Math.cos((Math.PI * (s - warm)) / (total - warm)))))
const bar = (f) => '█'.repeat(Math.round(f * 20)).padEnd(20, ' ')

const prompts = [
  'Explain why the sky is blue in two sentences.',
  'Write a haiku about a GPU that never sleeps.',
  'What is the capital of Australia, and why not Sydney?',
]
const answers = [
  ['The sky is blue blue because the the sky light is is blue.', 'GPU GPU warm / fans fans fans spinning spinning / the the night', 'The capital of Australia is Sydney Sydney, the capital.'],
  ['Sunlight scatters off air molecules, and blue light scatters most, so the sky looks blue.', 'Fans hum through the night / gradients fall like snow / loss creeps toward dawn', 'Canberra. It was built as a compromise between Sydney and Melbourne.'],
  [
    'Air molecules scatter short wavelengths far more than long ones (Rayleigh scattering), so blue light reaches your eyes from every direction of the sky.',
    'Fans hum, never still / a thousand gradients fall / dawn finds the loss low',
    'Canberra. Sydney and Melbourne both wanted it, so in 1908 a new city was chosen between them.',
  ],
]

function sft({ plateau, lr, tps, total, epochs = 2, examples = 207865, full = false }) {
  const evalEvery = 200
  return {
    every: 10,
    begin(r, t) {
      line(r, t, `Loading Qwen/Qwen3-8B in bf16 on ${r.gpus.length} GPU${r.gpus.length > 1 ? 's' : ''}`)
      line(r, t + 2 * S, full ? 'DeepSpeed ZeRO-3: 8,190,735,360 parameters, all trainable' : 'trainable params: 43,646,976 || all params: 8,234,382,336 || trainable%: 0.5301')
      line(r, t + 3 * S, '***** Running training *****')
      line(r, t + 3 * S, `  Num examples = ${examples.toLocaleString('en-US')}`)
      line(r, t + 3 * S, `  Num Epochs = ${epochs}`)
      line(r, t + 3 * S, `  Total optimization steps = ${total.toLocaleString('en-US')}`)
      line(r, t + 4 * S, 'UserWarning: torch.utils.checkpoint: the use_reentrant parameter should be passed explicitly.', 2)
    },
    at(r, s, t) {
      const x = r.seed
      const creep = r.nanAt != null ? Math.max(0, 1 - (r.nanAt - s) / 150) ** 2 : 0 // the loss climbs before it breaks
      const loss = p5(plateau + 1.35 * Math.exp(-s / 420) + 0.045 * g(x, s) + creep * 2.5)
      const rate = cosine(s, total, lr, 100)
      const grad = p5(0.32 + 1.1 * Math.exp(-s / 250) + 0.05 * Math.abs(g(x + 5, s)) + creep * 9)
      const epoch = +((s / total) * epochs).toFixed(2)
      put(r, 'train/loss', s, loss)
      put(r, 'train/learning_rate', s, p5(rate))
      put(r, 'train/grad_norm', s, grad)
      put(r, 'train/tokens_per_s', s, Math.round(tps + 70 * g(x + 6, s)))
      line(r, t, `{'loss': ${loss.toFixed(4)}, 'grad_norm': ${grad.toFixed(3)}, 'learning_rate': ${rate.toExponential(3)}, 'epoch': ${epoch}}`)
      if (s % 100 === 0) line(r, t, `${String(Math.floor((100 * s) / total)).padStart(3)}%|${bar(s / total)}| ${s}/${total} [${dur(s * r.spp)}<${dur((total - s) * r.spp)}, ${(r.spp / S).toFixed(2)}s/it]`)
      if (s % evalEvery === 0) {
        const ev = p5(plateau + 0.07 + 1.15 * Math.exp(-s / 520) + 0.008 * g(x + 7, s) + creep * 3)
        put(r, 'eval/loss', s, ev)
        line(r, t, `{'eval_loss': ${ev.toFixed(4)}, 'eval_runtime': ${(41 + u(x, s) * 3).toFixed(1)}, 'epoch': ${epoch}}`)
      }
      if (s % 1000 === 0) {
        artifact(r, `checkpoint-${s}`, 'checkpoint', s, full ? 16.4e9 : 175.2e6, t)
        line(r, t, `Saving model checkpoint to /data/runs/${r.name}/checkpoint-${s}`)
      }
      if (s % 500 === 0) {
        const q = Math.min(2, Math.floor(s / 1500))
        const rows = prompts.map((prompt, i) => ({ prompt, response: answers[q][i] }))
        media(r, 'generations', s, 'text', URL.createObjectURL(new Blob([JSON.stringify(rows)], { type: 'application/json' })), {}, t)
      }
    },
    nan(r, s, t) {
      put(r, 'train/loss', s, 'NaN')
      put(r, 'train/grad_norm', s, 'Infinity')
      line(r, t, `{'loss': nan, 'grad_norm': inf, 'learning_rate': ${cosine(s, total, lr, 100).toExponential(3)}}`)
      line(r, t + S, 'Traceback (most recent call last):', 2)
      line(r, t + S, '  File "/data/src/train_sft.py", line 212, in <module>', 2)
      line(r, t + S, '    trainer.train()', 2)
      line(r, t + S, 'ValueError: loss is NaN at step ' + s + '; stopping (runyard check: nan train/loss)', 2)
    },
    done(r, s, t) {
      line(r, t, `Training completed. Saving the final adapter to /data/runs/${r.name}/final`)
      artifact(r, 'final', 'model', s, full ? 16.4e9 : 175.2e6, t)
    },
  }
}

const scenes = ['a castle at dusk', 'a pine forest at night', 'a beach at noon', 'a mountain lake']
function diffusion() {
  return {
    every: 20,
    begin(r, t) {
      line(r, t, 'DiT-S/2 at 32x32, 32.9M parameters, EMA 0.9999, batch 256')
      line(r, t + S, 'dataset: 48,210 sprites from /data/pixel32 (4 classes)')
    },
    at(r, s, t) {
      const x = r.seed
      const loss = p5(0.027 + 0.095 * Math.exp(-s / 9000) + 0.0035 * g(x, s))
      put(r, 'train/loss', s, loss)
      put(r, 'train/lr', s, p5(s < 1000 ? (1e-4 * s) / 1000 : 1e-4))
      if (s % 100 === 0) line(r, t, `step ${s.toLocaleString('en-US')} | loss ${loss.toFixed(4)} | lr 1.00e-04 | ${(S / r.spp).toFixed(1)} it/s`)
      if (s % 2000 === 0) {
        const fid = p5(17.5 + 165 * Math.exp(-s / 21000) + 0.7 * g(x + 3, s))
        put(r, 'eval/fid', s, fid)
        scenes.forEach((scene, i) => media(r, 'samples', s, 'image', sprite(i * 13 + 5, s), { caption: scene }, t))
        line(r, t, `saved 4 samples at step ${s.toLocaleString('en-US')}, FID ${fid.toFixed(1)}`)
      }
      if (s % 10000 === 0) {
        artifact(r, `ema-${s}.pt`, 'checkpoint', s, 132.1e6, t)
        line(r, t, `checkpoint: /data/runs/${r.name}/ema-${s}.pt`)
      }
    },
  }
}

function cifar({ arch, lr, epochs, accMax }) {
  const per = 400
  return {
    every: 50,
    begin(r, t) {
      line(r, t, `${arch} on CIFAR-10, ${epochs} epochs, SGD lr ${lr} momentum 0.9, cosine`)
    },
    at(r, s, t) {
      const x = r.seed
      const ep = s / per
      const rate = cosine(s, epochs * per, lr, per)
      const loss = p5(0.05 + 2.1 * Math.exp(-ep / (7 * Math.sqrt(0.1 / lr))) + 0.06 * Math.abs(g(x, s)))
      put(r, 'train/loss', s, loss)
      put(r, 'train/lr', s, p5(rate))
      if (s % per === 0) {
        const acc = p5(Math.min(0.995, accMax * (1 - Math.exp(-ep / 9)) + 0.004 * g(x + 2, s)))
        const vloss = p5(0.17 + 1.9 * Math.exp(-ep / 8) + 0.02 * g(x + 3, s))
        put(r, 'val/acc', s, acc)
        put(r, 'val/loss', s, vloss)
        line(r, t, `Epoch ${ep}/${epochs} [${bar(ep / epochs)}] loss ${loss.toFixed(3)} | val_loss ${vloss.toFixed(3)} val_acc ${acc.toFixed(4)} | lr ${rate.toFixed(4)}`)
      }
    },
    nan(r, s, t) {
      put(r, 'train/loss', s, 'NaN')
      line(r, t, `Epoch ${Math.ceil(s / per)}: loss nan`, 2)
      line(r, t + S, 'RuntimeError: loss is NaN; stopping (runyard check: nan train/loss)', 2)
    },
    done(r, s, t) {
      artifact(r, 'model.pt', 'model', s, arch === 'ResNet-50' ? 94.4e6 : 44.8e6, t)
      line(r, t, `best val_acc ${r.sums['val/acc'].max.toFixed(4)}; saved model.pt`)
    },
  }
}

function quick() {
  return {
    every: 10,
    begin(r, t) {
      line(r, t, `MLP 784-256-10 on MNIST, lr ${r.config.lr}, batch ${r.config.batch_size}`)
    },
    at(r, s, t) {
      const x = r.seed
      const loss = p5(0.06 + 2.2 * Math.exp(-s / (180 * (0.003 / r.config.lr) ** 0.5)) + 0.04 * Math.abs(g(x, s)))
      put(r, 'train/loss', s, loss)
      if (s % 100 === 0) {
        const acc = p5(Math.min(0.99, 0.981 * (1 - Math.exp(-s / 260)) + 0.003 * g(x + 1, s)))
        put(r, 'val/acc', s, acc)
        line(r, t, `step ${s} | loss ${loss.toFixed(4)} | val_acc ${acc.toFixed(4)}`)
      }
    },
    done(r, s, t) {
      line(r, t, 'done')
    },
  }
}

// sprite draws a 32x32 sample of a scene the way a diffusion model gets there: coarse colour blobs under noise early in
// training, then finer detail as the noise fades.
const skies = [
  [[40, 30, 90], [240, 120, 90]],
  [[10, 14, 40], [40, 60, 110]],
  [[90, 170, 240], [200, 230, 250]],
  [[120, 160, 220], [230, 200, 170]],
]
function scene(kind, seed, x, y) {
  const n = 32
  const [top, low] = skies[kind]
  const sun = [8 + (seed % 16), 7 + (seed % 5)]
  let px = top.map((v, i) => v + ((low[i] - v) * y) / n)
  if ((x - sun[0]) ** 2 + (y - sun[1]) ** 2 < 10) px = kind === 1 ? [230, 230, 210] : [255, 220, 120]
  if (kind === 1 && u(seed, x * 97 + y) > 0.97 && y < 18) px = [220, 220, 255]
  const hill = 19 + 3 * Math.sin(x / 4 + seed)
  const front = 25 + 2 * Math.sin(x / 2.5 + seed * 2)
  if (y > hill) px = kind === 2 ? [230, 210, 150] : [40, 90 + kind * 10, 60]
  if (y > front) px = kind === 2 ? [40, 120, 200] : kind === 3 ? [50, 90, 160] : [30, 60, 40]
  if (kind === 0 && x >= 20 && x <= 27 && y >= 11 && y <= hill && !(y < 14 && x % 2)) px = [70, 60, 80]
  if (kind === 1 && x % 7 === 3 && y > hill - 7 && y <= hill + 2) px = [20, 50, 30]
  return px
}
function sprite(seed, step) {
  const n = 32
  const c = document.createElement('canvas')
  c.width = c.height = n
  const ctx = c.getContext('2d')
  const img = ctx.createImageData(n, n)
  const learned = 1 - Math.exp(-step / 9000)
  const block = learned < 0.25 ? 8 : learned < 0.5 ? 4 : learned < 0.75 ? 2 : 1
  const amp = 255 * 0.9 * (1 - learned)
  const kind = (seed - 5) / 13
  for (let y = 0; y < n; y++)
    for (let x = 0; x < n; x++) {
      const px = scene(kind, seed, x - (x % block), y - (y % block))
      const o = (y * n + x) * 4
      for (let i = 0; i < 3; i++) img.data[o + i] = Math.max(0, Math.min(255, Math.round(px[i] + amp * (u(seed * 3 + i + step, x * n + y) - 0.5))))
      img.data[o + 3] = 255
    }
  ctx.putImageData(img, 0, 0)
  return c.toDataURL()
}

// ---- the machines ----

const A100 = { name: 'NVIDIA A100-SXM4-80GB', mem_total_gb: 85.9, power_max_w: 400, idle: { util: [0, 0], mem: 0.6, temp: [31, 34], power: [52, 60] } }
const RTX = { name: 'NVIDIA GeForce RTX 4090', mem_total_gb: 25.8, power_max_w: 450, idle: { util: [0, 0], mem: 0.4, temp: [33, 37], power: [18, 26] } }
const TITAN = { name: 'NVIDIA TITAN RTX', mem_total_gb: 25.8, power_max_w: 280, idle: { util: [0, 0], mem: 0.3, temp: [35, 38], power: [15, 20] } }
const serve = { util: [12, 58], mem: 61.8, temp: [44, 55], power: [120, 230] }
const machines = new Map(
  [
    { name: 'a100-node', cores: 64, ram: 18, ram_total_gb: 1081.4, disk_gb: 2210, disk_total_gb: 3840, gpus: [A100, A100, A100, A100], extra: { 3: { proc: 'python serve.py --model qwen3-32b', user: 'lee', pid: 48211, prof: serve } } },
    { name: 'lab-4090', cores: 32, ram: 9, ram_total_gb: 134.9, disk_gb: 1430, disk_total_gb: 1999, gpus: [RTX, RTX] },
    { name: 'macbook-pro', cores: 12, ram: 21, ram_total_gb: 38.7, disk_gb: 611, disk_total_gb: 994, gpus: [] },
    { name: 'old-titan', cores: 16, ram: 6, ram_total_gb: 67.3, disk_gb: 820, disk_total_gb: 999, gpus: [TITAN], gone: T0 - 3 * H - 12 * M },
  ].map((m) => [m.name, m]),
)

// load is what a machine's beacon reads at time t: whatever runs on each GPU then, or the idle card.
function load(m, t) {
  const i = Math.floor(t / (10 * S))
  const on = [...runs.values()].filter((r) => r.host === m.name && r.start <= t && t <= endMs(r))
  const x = m.name.length * 101
  let cpu = 3 + 4 * u(x, i)
  let ram = m.ram + 0.3 * g(x + 1, i)
  const gpus = m.gpus.map((card, gi) => {
    const r = on.find((r) => r.gpus.includes(gi))
    const extra = m.extra?.[gi]
    const p = r ? r.prof : extra ? extra.prof : card.idle
    const v = gpuLoad(p, (r ? r.seed : x) + gi, i)
    const procs = r
      ? [{ pid: r.pid + r.gpus.indexOf(gi), name: r.proc, user: r.user, mem_gb: p5(v.mem_gb - 0.4) }]
      : extra
        ? [{ pid: extra.pid, name: extra.proc, user: extra.user, mem_gb: p5(v.mem_gb - 0.4) }]
        : []
    return { i: gi, name: card.name, util: v.util, mem_gb: v.mem_gb, mem_total_gb: card.mem_total_gb, temp_c: v.temp_c, power_w: v.power_w, power_max_w: card.power_max_w, procs }
  })
  for (const r of on) {
    cpu += within(r.prof.cpu, r.seed + 10, i) / (r.gpus.length ? 2 : 1)
    ram += r.prof.ram
  }
  if (m.extra) (cpu += 6), (ram += 24)
  return { cpu: p5(Math.min(100, cpu)), cores: m.cores, load: p5((cpu / 100) * m.cores), ram_gb: p5(ram), ram_total_gb: m.ram_total_gb, disk_gb: m.disk_gb, disk_total_gb: m.disk_total_gb, gpus }
}
const seen = (m, now) => Math.min(m.gone ?? Infinity, Math.floor(now / (10 * S)) * 10 * S)

function hostOut(m, now) {
  const t = seen(m, now)
  return {
    name: m.name,
    seen_at: iso(t),
    silent: now - t > 2 * M,
    stats: load(m, t),
    runs: [...runs.values()].filter((r) => r.host === m.name && r.status === 'running').map((r) => ({ id: r.id, name: r.name, project: r.project })),
  }
}

function history(m, hours, now) {
  const bucket = hours * 10 * S
  const ts = []
  const values = {}
  const keys = ['cpu', 'ram_gb', 'disk_gb', ...m.gpus.flatMap((_, i) => ['util', 'mem_gb', 'temp_c', 'power_w'].map((k) => `gpu${i}.${k}`))]
  for (const k of keys) values[k] = []
  const last = seen(m, now)
  for (let t = Math.ceil((now - hours * H) / bucket) * bucket; t <= last; t += bucket) {
    const l = load(m, t)
    ts.push(Math.floor(t / S))
    values.cpu.push(l.cpu)
    values.ram_gb.push(l.ram_gb)
    values.disk_gb.push(l.disk_gb)
    for (const gp of l.gpus) for (const k of ['util', 'mem_gb', 'temp_c', 'power_w']) values[`gpu${gp.i}.${k}`].push(gp[k])
  }
  return { ts, values }
}

// ---- the story: runs from the last days, three still training, and what happens in the next minutes ----

const sftProf = { util: [91, 99], mem: 58.3, temp: [61, 69], power: [330, 395], cpu: [14, 26], ram: 46 }
const fullProf = { util: [94, 100], mem: 79.1, temp: [66, 74], power: [360, 400], cpu: [18, 30], ram: 88 }
const ditProf = { util: [95, 100], mem: 14.2, temp: [70, 78], power: [380, 440], cpu: [9, 16], ram: 12 }
const resProf = { util: [86, 97], mem: 9.7, temp: [64, 72], power: [300, 380], cpu: [20, 32], ram: 8 }
const cpuProf = { cpu: [55, 80], ram: 3.1 }
const qwen = (o) => ({ model: 'Qwen/Qwen3-8B', dataset: 'ultrachat_200k', seq_len: 4096, batch_size: 64, epochs: 2, ...o })

const live = []
function story() {
  const r16 = qwen({ lora: { r: 16, alpha: 32, dropout: 0.05, target: 'all-linear' }, lr: 2e-4 })
  addRun({ seed: 11, project: 'qwen-sft', name: 'qwen3-8b-lora-r64', host: 'a100-node', gpus: [0], start: T0 - 27 * H, secPerStep: 1.9, endStep: 6000, model: sft({ plateau: 1.05, lr: 1e-4, tps: 2950, total: 6000 }), prof: { ...sftProf, mem: 66.9 }, proc: 'python train_sft.py', user: 'ana', config: qwen({ lora: { r: 64, alpha: 128, dropout: 0.05, target: 'all-linear' }, lr: 1e-4 }), tags: ['lora', 'r64'], notes: 'Rank 64: eval/loss 0.06 below r16 for 4x the adapter.' })
  addRun({ seed: 12, project: 'qwen-sft', name: 'qwen3-8b-lora-r16-nopack', host: 'a100-node', gpus: [1], start: T0 - 2 * D - 5 * H, secPerStep: 1.4, endStep: 2010, fate: 'crashed', model: sft({ plateau: 1.13, lr: 2e-4, tps: 2400, total: 6000 }), prof: sftProf, proc: 'python train_sft.py', user: 'kai', config: { ...r16, packing: false }, tags: ['lora', 'r16'], notes: 'Node rebooted for a driver update mid-run.' })
  live.push(addRun({ seed: 13, project: 'qwen-sft', name: 'qwen3-8b-lora-r16', host: 'a100-node', gpus: [0], start: T0 - 52 * M, secPerStep: 1.1, endStep: 6000, model: sft({ plateau: 1.12, lr: 2e-4, tps: 3410, total: 6000 }), prof: sftProf, proc: 'python train_sft.py', user: 'ana', config: { ...r16, packing: true }, tags: ['lora', 'r16', 'baseline'], notes: 'Baseline LoRA, rank 16 on every linear layer.' }))
  const full = addRun({ seed: 14, project: 'qwen-sft', name: 'qwen3-8b-full-lr2e-5', host: 'a100-node', gpus: [1, 2], start: T0 - 38 * M, secPerStep: 2.3, endStep: 6000, model: sft({ plateau: 1.0, lr: 2e-5, tps: 1840, total: 6000, full: true }), prof: fullProf, proc: 'torchrun train_sft.py --deepspeed zero3', user: 'kai', config: qwen({ lr: 2e-5, deepspeed: 'zero3', full_finetune: true }), tags: ['full'] })
  full.nanAt = Math.ceil(stepAt(full, T0 + 150 * S) / 10) * 10
  live.push(full)

  addRun({ seed: 21, project: 'pixel-diffusion', name: 'unet-baseline', host: 'lab-4090', gpus: [0], start: T0 - 4 * D, secPerStep: 0.21, endStep: 60000, model: diffusion(), prof: { ...ditProf, mem: 11.8 }, proc: 'python train_unet.py', user: 'ana', config: { model: 'UNet', channels: [64, 128, 256], resolution: 32, batch_size: 256, lr: 1e-4 }, tags: ['baseline'] })
  addRun({ seed: 22, project: 'pixel-diffusion', name: 'dit-s-pixel32-ema0.999', host: 'lab-4090', gpus: [0], start: T0 - 30 * H, secPerStep: 0.16, endStep: 80000, model: diffusion(), prof: ditProf, proc: 'python train_dit.py', user: 'ana', config: { model: 'DiT-S/2', resolution: 32, patch: 2, ema: 0.999, batch_size: 256, lr: 1e-4 }, tags: ['dit'] })
  live.push(addRun({ seed: 23, project: 'pixel-diffusion', name: 'dit-s-pixel32', host: 'lab-4090', gpus: [0], start: T0 - 3 * H - 20 * M, secPerStep: 0.15, endStep: 200000, model: diffusion(), prof: ditProf, proc: 'python train_dit.py', user: 'ana', config: { model: 'DiT-S/2', resolution: 32, patch: 2, ema: 0.9999, batch_size: 256, lr: 1e-4 }, tags: ['dit'], notes: 'EMA 0.9999; samples every 2,000 steps.', branch: 'dit', dirty: true }))

  const res = (o) => ({ dataset: 'cifar10', batch_size: 128, optimizer: 'sgd', momentum: 0.9, weight_decay: 5e-4, ...o })
  addRun({ seed: 31, project: 'cifar10-resnet', name: 'resnet18-lr0.1', host: 'lab-4090', gpus: [1], start: T0 - 3 * D, secPerStep: 0.045, endStep: 100 * 400, model: cifar({ arch: 'ResNet-18', lr: 0.1, epochs: 100, accMax: 0.953 }), prof: resProf, proc: 'python train.py', user: 'kai', config: res({ arch: 'resnet18', lr: 0.1, epochs: 100 }), tags: ['sweep-lr'] })
  addRun({ seed: 32, project: 'cifar10-resnet', name: 'resnet18-lr0.05', host: 'lab-4090', gpus: [1], start: T0 - 3 * D + 40 * M, secPerStep: 0.045, endStep: 100 * 400, model: cifar({ arch: 'ResNet-18', lr: 0.05, epochs: 100, accMax: 0.947 }), prof: resProf, proc: 'python train.py', user: 'kai', config: res({ arch: 'resnet18', lr: 0.05, epochs: 100 }), tags: ['sweep-lr'] })
  addRun({ seed: 33, project: 'cifar10-resnet', name: 'resnet18-lr0.4', host: 'lab-4090', gpus: [1], start: T0 - 2 * D - 20 * H, secPerStep: 0.045, endStep: 100 * 400, nanAt: 2350, model: cifar({ arch: 'ResNet-18', lr: 0.4, epochs: 100, accMax: 0.9 }), prof: resProf, proc: 'python train.py', user: 'kai', config: res({ arch: 'resnet18', lr: 0.4, epochs: 100 }), tags: ['sweep-lr'] })
  const r50 = addRun({ seed: 34, project: 'cifar10-resnet', name: 'resnet50-cosine', host: 'lab-4090', gpus: [1], start: T0 + 75 * S - 200 * 400 * 0.06 * S, secPerStep: 0.06, endStep: 200 * 400, model: cifar({ arch: 'ResNet-50', lr: 0.1, epochs: 200, accMax: 0.962 }), prof: resProf, proc: 'python train.py --arch resnet50', user: 'kai', config: res({ arch: 'resnet50', lr: 0.1, epochs: 200, schedule: 'cosine' }), tags: ['resnet50'] })
  live.push(r50)
}

// Alerts from checks, at fixed moments: some in the past, one a minute into the visit.
const scripted = [
  { at: T0 - 26 * H, run: 'qwen3-8b-lora-r64', level: 'info', title: 'eval/loss below 1.15', body: 'check: eval/loss < 1.15, at step 4,200' },
  { at: T0 - 41 * M, run: 'dit-s-pixel32', level: 'warn', title: 'sys/gpu0.temp_c above 78', body: 'check: GPU 0 at 79 °C for 5 minutes' },
  { at: T0 + 50 * S, run: 'qwen3-8b-full-lr2e-5', level: 'warn', title: 'train/grad_norm above 4', body: 'check: train/grad_norm > 4; the loss is climbing too' },
]

function tick(now) {
  for (const r of runs.values()) {
    advance(r, now)
    for (const type of r.dirty) publish({ type, run: r.id })
    r.dirty.clear()
    if (r.status === 'running' && now - (r.beat || 0) >= 30 * S) (r.beat = now), publish({ type: 'run', run: r.id }) // the SDK's heartbeat
  }
  for (const a of scripted)
    if (!a.done && a.at <= now) {
      a.done = true
      const r = [...runs.values()].find((r) => r.name === a.run)
      if (r) alert(r, a.at, a.level, a.title, a.body)
    }
}

story()
tick(T0)
for (const r of runs.values()) r.dirty.clear()
booting = false
setInterval(() => tick(Date.now()), 1000)
setInterval(() => publish({ type: 'host' }), 10000)

// startRun is the demo's "Start a run" button: a small run on your laptop that finishes in about a minute.
let mine = 0
export function startRun() {
  const now = Date.now()
  const lr = [1e-3, 3e-3, 1e-2][mine % 3]
  const r = addRun({ seed: 1000 + ++mine, project: 'playground', name: `my-run-${mine}`, host: 'your-laptop', start: now, secPerStep: 0.05, endStep: 1200, model: quick(), prof: cpuProf, proc: 'python train.py', user: 'you', config: { model: 'mlp', hidden: 256, lr, batch_size: 128, epochs: 3 }, tags: ['demo'] })
  r.dirty.clear()
  publish({ type: 'run', run: r.id })
  return r.id
}

// ---- the API ----

const pub = (r, withMetrics) => {
  const { id, project, name, status, config, summary, tags, notes, host, git, started_at, ended_at, last_heartbeat_at, agent } = r
  return { id, project, name, status, config, summary, tags, notes, host, git, started_at, ended_at, last_heartbeat_at, agent, ...(withMetrics && { metrics: r.sums }) }
}
const byStart = (a, b) => b.start - a.start

function agentOut(a, now) {
  const rs = [...runs.values()].filter((r) => r.agent === a.id).sort(byStart)
  const last = rs[0]
  const running = rs.filter((r) => r.status === 'running').length
  const state = a.waiting ? 'waiting' : running ? 'training' : last?.status === 'finished' ? 'done' : 'error'
  return {
    id: a.id, tool: a.tool, label: '', host: a.host, cwd: a.cwd, project: a.project, metric: a.metric, goal: a.goal, state, said: '',
    report: a.report, next: a.next || '', report_run: last?.name ?? null, report_at: iso(now - 4 * M), runs: rs.length, running,
    kept: rs.filter((r) => r.tags.includes('keep')).length, last_run: last ? { id: last.id, name: last.name, status: last.status, ended_at: last.ended_at } : null,
    started_at: rs.at(-1)?.started_at, active_at: iso(now - 4 * M),
  }
}

// series cuts a key's history into at most n buckets of equal step width, as the server does.
function series(rows, n) {
  if (rows.length <= n) return { step: rows.map((x) => x[0]), min: rows.map((x) => x[1]), max: rows.map((x) => x[1]), last: rows.map((x) => x[1]) }
  const lo = rows[0][0]
  const width = Math.ceil((rows.at(-1)[0] - lo + 1) / n)
  const out = { step: [], min: [], max: [], last: [] }
  let b = -1
  for (const [s, v] of rows) {
    const k = Math.floor((s - lo) / width)
    if (k !== b) {
      b = k
      out.step.push(s), out.min.push(v), out.max.push(v), out.last.push(v)
      continue
    }
    const i = out.step.length - 1
    out.step[i] = s
    out.last[i] = v
    if (typeof v === 'number') {
      out.min[i] = typeof out.min[i] === 'number' ? Math.min(out.min[i], v) : v
      out.max[i] = typeof out.max[i] === 'number' ? Math.max(out.max[i], v) : v
    }
  }
  return out
}

const notFound = { status: 404, body: { error: 'not found' } }
function route(method, path, q, body) {
  const now = Date.now()
  const p = path.split('/').filter(Boolean).map(decodeURIComponent)
  const run = p[0] === 'runs' && p[1] ? runs.get(p[1]) : null
  const ok = (b) => ({ status: 200, body: b })

  if (method === 'GET') {
    if (path === '/me') return ok({ auth: false, user: '' })
    if (path === '/projects')
      return ok(
        [...projects.values()]
          .map((pr) => {
            const rs = [...runs.values()].filter((r) => r.project === pr.name)
            const last = Math.max(...rs.map((r) => r.start))
            return { ...pr, runs: rs.length, running: rs.filter((r) => r.status === 'running').length, last_run_at: rs.length ? iso(last) : null }
          })
          .sort((a, b) => (b.last_run_at || '').localeCompare(a.last_run_at || '')),
      )
    if (path === '/runs') {
      let rs = [...runs.values()].sort(byStart)
      if (q.get('project')) rs = rs.filter((r) => r.project === q.get('project'))
      if (q.get('status')) rs = rs.filter((r) => r.status === q.get('status'))
      const find = (q.get('q') || '').toLowerCase()
      if (find) rs = rs.filter((r) => [r.name, ...(r.tags || [])].join(' ').toLowerCase().includes(find))
      if (q.get('sort') === 'ended') rs.sort((a, b) => (b.ended_at || '').localeCompare(a.ended_at || ''))
      return ok(rs.slice(0, +q.get('limit') || 100).map((r) => pub(r, q.get('metrics') === '1')))
    }
    if (run && p.length === 2) return ok(pub(run, true))
    if (run && p[2] === 'metrics') {
      const want = q.get('keys')?.split(',').filter(Boolean)
      const n = Math.min(+q.get('points') || 600, 2000)
      return ok(Object.fromEntries(Object.entries(run.series).filter(([k]) => !want || want.includes(k)).map(([k, rows]) => [k, series(rows, n)])))
    }
    if (run && p[2] === 'logs') {
      const limit = +q.get('limit') || 1000
      const before = +q.get('before') || Infinity
      if (q.has('tail')) return ok(run.lines.filter((l) => l.seq < before).slice(-limit))
      const after = +q.get('after') || 0
      return ok(run.lines.filter((l) => l.seq > after).slice(0, limit))
    }
    if (run && p[2] === 'media') return ok(run.media.filter((m) => !q.get('kind') || m.kind === q.get('kind')))
    if (run && p[2] === 'artifacts') return ok([...run.artifacts].reverse())
    if (path === '/alerts') {
      let as = alerts.map((a) => ({ ...a, run_name: runs.get(a.run)?.name, project: runs.get(a.run)?.project })).filter((a) => a.run_name)
      if (q.get('run')) as = as.filter((a) => a.run === q.get('run'))
      if (q.get('project')) as = as.filter((a) => a.project === q.get('project'))
      if (q.has('active')) as = as.filter((a) => !a.dismissed_at)
      return ok(as.slice(0, +q.get('limit') || 100))
    }
    if (path === '/agents') return ok(agents.filter((a) => !q.get('project') || a.project === q.get('project')).map((a) => agentOut(a, now)))
    if (path === '/hosts') return ok([...machines.values()].map((m) => hostOut(m, now)).sort((a, b) => a.name.localeCompare(b.name)))
    if (p[0] === 'hosts' && p[2] === 'history') {
      const m = machines.get(p[1])
      return m ? ok(history(m, Math.min(24, Math.max(1, +q.get('hours') || 1)), now)) : notFound
    }
    if (path === '/keys') return ok(keys)
    if (path === '/channels') return ok(channels)
    if (path === '/push/key') return { status: 404, body: { error: 'In the demo, notifications show up in the page.' } }
    return notFound
  }

  if (method === 'PATCH' && run && p[2] === 'meta') {
    if (typeof body.notes === 'string') run.notes = body.notes
    if (Array.isArray(body.tags)) run.tags = [...new Set(body.tags.map((t) => t.trim()).filter(Boolean))]
    publish({ type: 'run', run: run.id })
    return ok(pub(run))
  }
  if (method === 'POST' && path === '/alerts/dismiss') {
    let n = 0
    for (const a of alerts) if (!a.dismissed_at && (!body.ids?.length || body.ids.includes(a.id))) (a.dismissed_at = iso(now)), n++
    publish({ type: 'alert' })
    return ok({ dismissed: n })
  }
  if (method === 'DELETE' && run) {
    runs.delete(run.id)
    publish({ type: 'run' })
    return ok({})
  }
  if (method === 'PATCH' && p[0] === 'projects') {
    Object.assign(projects.get(p[1]) || {}, { metric: body.metric, goal: body.goal })
    return ok({})
  }
  if (method === 'DELETE' && p[0] === 'projects') {
    for (const r of runs.values()) if (r.project === p[1]) runs.delete(r.id)
    projects.delete(p[1])
    publish({ type: 'run' })
    return ok({})
  }
  if (method === 'DELETE' && p[0] === 'hosts') {
    machines.delete(p[1])
    publish({ type: 'host' })
    return ok({})
  }
  if (method === 'POST' && path === '/keys') {
    const k = { id: nextId++, name: body.name || 'key', user: body.user || '', created_at: iso(now) }
    keys.push(k)
    return ok({ ...k, key: 'ry_demo_' + hex(now % 1e6, 24) })
  }
  if (method === 'DELETE' && p[0] === 'keys') {
    keys.splice(keys.findIndex((k) => k.id === +p[1]) >>> 0, 1)
    return ok({})
  }
  if (method === 'POST' && path === '/channels') {
    if (!body.target) return { status: 400, body: { error: 'target is required' } }
    const c = { id: nextId++, kind: body.kind, target: body.target, project: body.project || '', events: body.events || [] }
    channels.push(c)
    return ok(c)
  }
  if (method === 'POST' && p[0] === 'channels' && p[2] === 'test') {
    const c = channels.find((c) => c.id === +p[1])
    notice({ level: 'info', title: 'Test from runyard', body: `This ${c?.kind || ''} channel works. On your own server it would land there, not in this page.` })
    return ok({})
  }
  if (method === 'DELETE' && p[0] === 'channels') {
    channels.splice(channels.findIndex((c) => c.id === +p[1]) >>> 0, 1)
    return ok({})
  }
  if (path === '/logout' || path.startsWith('/push/')) return ok({})
  return notFound
}

// ---- in place of the network ----

const realFetch = window.fetch.bind(window)
window.fetch = async (input, init = {}) => {
  const url = new URL(typeof input === 'string' ? input : input.url, location.href)
  if (url.origin !== location.origin || !url.pathname.startsWith('/api/v1/')) return realFetch(input, init)
  await new Promise((r) => setTimeout(r, 30 + Math.random() * 90)) // about a network's worth
  let out
  try {
    out = route((init.method || 'GET').toUpperCase(), url.pathname.slice(7), url.searchParams, init.body ? JSON.parse(init.body) : {})
  } catch (e) {
    out = { status: 500, body: { error: String(e) } }
  }
  return new Response(JSON.stringify(out.body), { status: out.status, headers: { 'Content-Type': 'application/json' } })
}
const RealEventSource = window.EventSource
window.EventSource = function (url, opts) {
  return String(url).includes('/api/v1/events') ? new DemoEventSource() : new RealEventSource(url, opts)
}
// The bell asks the browser for push; the demo has no service worker, so it says where notifications go instead.
if (navigator.serviceWorker) navigator.serviceWorker.register = () => Promise.reject(new Error('In the demo, notifications show up in the page.'))
// Settings acts with an API key on a server without users; the demo hands it one.
try {
  sessionStorage.getItem('runyard.key') ?? sessionStorage.setItem('runyard.key', 'ry_demo')
} catch {}
