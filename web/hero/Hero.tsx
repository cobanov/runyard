import { Stage, type Act } from './kit/stage'
import { Window, Bar } from './kit/window'
import { Phone, LockScreen, Notification } from './kit/phone'
import { Count } from './kit/motion/Count'
import { Roll } from './kit/motion/Roll'
import { Collapse } from './kit/motion/Collapse'
import './hero.css'

// The website's hero: runyard's Overview with runs training live, and a phone
// beside it. A full fine-tune on a100-node dies without a word: its two GPUs
// fall to idle under it, its heartbeat stops, the card turns Crashed, and the
// phone on the left wakes with the notification runyard sends for it. Then
// the run is restarted: the alert goes, the card comes back Running and the
// loop goes on from there. No cursor: this is what the system does while
// nobody is looking.
//
// Real words: the two run cards, the a100-node GPU table, the notifications.
// Everything the phone covers (the sidebar's nav) is real too but hidden.

type S = {
  // Live updates so far on each card. They only count up, so the numbers never
  // rewind at the seam; left out of initial, the seam check skips them
  n?: number // the fine-tune
  m?: number // the DiT
  dead: boolean // the process is gone: its GPUs fall idle, its numbers stop
  beat: number // how long since its last heartbeat
  crashed: boolean
  alerted: boolean
}

const initial: S = { dead: false, beat: 0, crashed: false, alerted: false }
const poster: S = {
  n: 2,
  m: 3,
  dead: true,
  beat: 3,
  crashed: true,
  alerted: true,
}

// train/loss and step after n live updates: a slow plateau with a wobble
const full = (n = 0) => ({
  loss: 0.9 + 0.142 * 0.997 ** n + [0, -0.002, 0.001][n % 3],
  step: 3240 + 20 * n,
})
const dit = (m = 0) => ({
  loss: 0.028 + 0.00087 * 0.998 ** m + [0, -0.00003, -0.00001, -0.00006][m % 4],
  step: 80020 + 40 * m,
})
const both = (s: S) => ({ n: (s.n ?? 0) + 1, m: (s.m ?? 0) + 1 })
const BEATS = ['0s', '1m', '3m', '5m']

// The two lines the sparklines draw: bucketed loss over the whole run
const FULL_SPARK = [
  2.31, 1.86, 1.62, 1.49, 1.41, 1.35, 1.3, 1.26, 1.22, 1.19, 1.17, 1.15, 1.13, 1.12, 1.1, 1.09, 1.08, 1.07, 1.06, 1.06, 1.05, 1.05, 1.04, 1.04,
]
const DIT_SPARK = [
  0.071, 0.052, 0.046, 0.043, 0.041, 0.04, 0.038, 0.037, 0.036, 0.035, 0.034, 0.034, 0.033, 0.032, 0.032, 0.031, 0.031, 0.03, 0.03, 0.03, 0.029, 0.029, 0.029,
  0.029,
]

// a100-node: GPUs 0 and 1 are the fine-tune's, 2 and 3 belong to others.
// Their readings roll as text, so a figure losing a digit eases its width
const GPUS = [
  { util: 97, mem: 71.2, temp: 68, power: 372, run: true },
  { util: 98, mem: 71.6, temp: 70, power: 381, run: true },
  { util: 92, mem: 58.3, temp: 65, power: 355 },
  { util: 35, mem: 61.8, temp: 50, power: 135 },
]
const IDLE = { util: 0, mem: 0.6, power: 58 }

// runyard's fmt: four significant figures
const fmt = (n: number) => String(+n.toPrecision(4))
const int = (n: number) => Math.round(n).toLocaleString('en-US')

async function script(a: Act<S>) {
  await a.wait(1000)
  a.put(both)
  await a.wait(1100)
  a.put(both)
  await a.wait(1000)
  // The process dies between two updates: GPUs 0 and 1 drop to idle
  a.mark('dead')
  a.put({ dead: true })
  await a.wait(900)
  a.put((s) => ({ m: (s.m ?? 0) + 1 }))
  // Nothing more arrives from it; its heartbeat ages until runyard calls it
  for (let b = 1; b < BEATS.length; b++) {
    await a.wait(520)
    a.put({ beat: b })
  }
  await a.wait(700)
  a.mark('crashed')
  a.put({ crashed: true })
  await a.wait(450)
  a.mark('alert')
  a.put({ alerted: true })
  await a.wait(2800)
  // Restarted: what the crash added leaves in reverse, the alert first
  a.mark('restart')
  a.put({ alerted: false })
  await a.wait(80)
  a.put({ crashed: false, beat: 0 })
  await a.wait(80)
  a.put({ dead: false })
  await a.wait(100)
}

export default function Hero({ theme }: { theme: 'light' | 'dark' }) {
  return (
    <Stage<S>
      width={1000}
      height={560}
      compact={{ below: 700, width: 350, height: 550 }}
      theme={theme}
      fidelity="realistic"
      play="loop"
      initial={initial}
      poster={poster}
      script={script}
      className="ry-hero"
    >
      {(s, _rect, layout) => {
        const f = full(s.n)
        const d = dit(s.m)
        return (
          <>
            <Window className="ry-win" title="Overview" sidebar={layout === 'wide' ? <Side /> : undefined} sidebarWidth={188}>
              <div className="ry-body">
                <div className="ry-cards">
                  <RunCard
                    project="sft"
                    name="qwen3-8b-full"
                    value={f.loss}
                    step={f.step}
                    spark={FULL_SPARK}
                    took="2h 41m"
                    beat={BEATS[s.beat]}
                    host="a100-node"
                    crashed={s.crashed}
                  />
                  {layout === 'wide' && (
                    <RunCard
                      project="pixel-diffusion"
                      name="dit-s-pixel32"
                      value={d.loss}
                      step={d.step}
                      spark={DIT_SPARK}
                      took="3h 20m"
                      beat="0s"
                      host="lab-4090"
                      quiet
                    />
                  )}
                </div>
                {/* Narrow, the window holds the card alone */}
                {layout === 'wide' && (
                  <>
                    <div className="ry-h2">Machine load</div>
                    <div className="ry-hosts">
                      <Host dead={s.dead} />
                      <OtherHost />
                    </div>
                  </>
                )}
              </div>
            </Window>

            <Phone width={layout === 'wide' ? 300 : 318} frame="pro" dark wallpaper={WALL} time="3:12" className="ry-phone">
              <LockScreen size="satellite" stack="top" date="Sunday, November 1" time="3:12" clock={layout === 'wide'}>
                <Collapse open={s.alerted} reveal="drop">
                  <Notification
                    icon={<AppIcon />}
                    title="sft/qwen3-8b-full went silent"
                    body="No heartbeat for 5m 0s after 2h 41m of running: the process likely died (killed, out of memory, machine down)."
                  />
                </Collapse>
                <Notification
                  icon={<AppIcon />}
                  title="cifar10/resnet18 finished"
                  body={'Ran for 1h 12m.\neval/acc: 0.953'}
                  time="1h ago"
                  className="ry-older"
                />
              </LockScreen>
            </Phone>
          </>
        )
      }}
    </Stage>
  )
}

// A quiet blue-gray lock screen, so the white clock doesn't outweigh the banner
const WALL = 'radial-gradient(120% 70% at 25% 0%, rgba(255, 255, 255, 0.3), transparent 65%), linear-gradient(172deg, #8eb0c2 0%, #95a9bd 50%, #a3aebd 100%)'

function RunCard(p: {
  project: string
  name: string
  value: number
  step: number
  spark: number[]
  took: string
  beat: string
  host: string
  crashed?: boolean
  quiet?: boolean
}) {
  return (
    <div className="ry-card ry-run" data-crashed={p.crashed || undefined} data-quiet={p.quiet || undefined}>
      <div className="ry-who">
        <span className="ry-muted">{p.project}</span>
        <span className="ry-badge">
          <i className="ry-dot" />
          <Roll align="end">{p.crashed ? 'Crashed' : 'Running'}</Roll>
        </span>
      </div>
      <span className="ry-name">{p.name}</span>
      <div className="ry-metric">
        <Count className="ry-value" value={p.value} format={fmt} />
        <span className="ry-muted">
          train/loss at step <Count value={p.step} format={int} />
        </span>
      </div>
      <Spark values={p.spark} />
      {p.quiet ? (
        // The neighbor the crop cuts: its last line is filler
        <div className="ry-meta">
          <Bar w={44} h={6} tone={3} />
          <Bar w={108} h={6} tone={3} />
          <Bar w={64} h={6} tone={3} />
        </div>
      ) : (
        <div className="ry-meta ry-muted">
          <span>{p.took}</span>
          <span>
            heartbeat <Roll>{p.beat}</Roll> ago
          </span>
          <span>{p.host}</span>
        </div>
      )}
    </div>
  )
}

// runyard's Sparkline: straight segments, 1.5px, no axes, no fill
function Spark({ values }: { values: number[] }) {
  const h = 36
  const lo = Math.min(...values)
  const span = Math.max(...values) - lo || 1
  const d = values
    .map((v, i) => `${i ? 'L' : 'M'}${((i / (values.length - 1)) * 100).toFixed(1)} ${(h - 2 - ((v - lo) / span) * (h - 4)).toFixed(1)}`)
    .join(' ')
  return (
    <svg className="ry-spark" viewBox={`0 0 100 ${h}`} preserveAspectRatio="none" height={h} aria-hidden>
      <path d={d} fill="none" stroke="var(--ry-c2)" strokeWidth="1.5" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  )
}

function Host({ dead }: { dead: boolean }) {
  return (
    <div className="ry-card ry-host">
      <div className="ry-who">
        <strong>a100-node</strong>
        <span className="ry-muted">CPU 35%, RAM 175.8 GB</span>
      </div>
      <div className="ry-gpus">
        <div className="ry-gpu ry-gpu--head ry-muted">
          <span>GPU</span>
          <span>Util</span>
          <span>Memory</span>
          <span>Temp</span>
          <span>Power</span>
        </div>
        {GPUS.map((g, i) => {
          const v = g.run && dead ? { ...g, ...IDLE } : g
          return (
            <div key={i} className="ry-gpu">
              <span>{i}</span>
              <span className="ry-util">
                <span className="ry-track">
                  <span className="ry-fill" style={{ transform: `scaleX(${v.util / 100})` }} />
                </span>
                <Roll>{`${v.util}%`}</Roll>
              </span>
              <span>
                <Roll>{`${v.mem.toFixed(1)} GB`}</Roll>
              </span>
              <span>{v.temp} °C</span>
              <span>
                <Roll>{`${v.power} W`}</Roll>
              </span>
            </div>
          )
        })}
      </div>
    </div>
  )
}

// lab-4090, cut by the stage's right edge after its Util column, so only
// the columns in frame are drawn
function OtherHost() {
  return (
    <div className="ry-card ry-host">
      <div className="ry-who">
        <strong>lab-4090</strong>
      </div>
      <div className="ry-gpus">
        <div className="ry-gpu ry-gpu--head ry-muted">
          <span>GPU</span>
          <span>Util</span>
        </div>
        {[96, 88, 41, 0].map((u, i) => (
          <div key={i} className="ry-gpu">
            <span>{i}</span>
            <span className="ry-util">
              <span className="ry-track">
                <span className="ry-fill" style={{ transform: `scaleX(${u / 100})` }} />
              </span>
              {u}%
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

const MARK_OUTER = 'M36 220 V144 A108 108 0 0 1 144 36 H220 L184 72 H144 A72 72 0 0 0 72 144 V220 Z'
const MARK_INNER = 'M96 220 V144 A48 48 0 0 1 144 96 H220 L184 132 H144 A12 12 0 0 0 132 144 V220 Z'

// The app icon: the lanes on black, as on the home screen
function AppIcon() {
  return (
    <svg viewBox="0 0 256 256" width="38" height="38" aria-hidden>
      <rect width="256" height="256" fill="#09090b" />
      <g transform="translate(36 36) scale(0.72)">
        <path fill="#fafafa" d={MARK_OUTER} />
        <path fill="#2dd4bf" d={MARK_INNER} />
      </g>
    </svg>
  )
}

// runyard's sidebar: the brand with its live dot, then the nav
const NAV = [
  ['Overview', 'M4 13h6V4H4zM14 20h6v-9h-6zM4 20h6v-4H4zM14 7h6V4h-6z'],
  ['Projects', 'M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z'],
  ['Machines', 'M4 5h16v10H4zM8 19h8M12 15v4M8 12V9M12 12V8M16 12v-2'],
  ['Alerts', 'M12 3 2 20h20zM12 10v4M12 17v.5'],
  ['Settings', 'M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6'],
]

function Side() {
  return (
    <nav className="ry-side">
      <span className="ry-brand">
        {/* The product's mark, not a nav icon */}
        <svg className="ss-brand" viewBox="0 0 256 256" aria-hidden>
          <path fill="currentColor" d={MARK_OUTER} />
          <path fill="var(--ry-running)" d={MARK_INNER} />
        </svg>
        runyard
        <i className="ry-live" />
      </span>
      {NAV.map(([label, d], i) => (
        <span key={label} className="ry-item" data-current={i === 0 || undefined}>
          <svg viewBox="0 0 24 24" aria-hidden>
            <path d={d} />
          </svg>
          {label}
        </span>
      ))}
    </nav>
  )
}
