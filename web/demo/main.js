// The demo: the real web UI, with mock.js answering in place of a server (imported first, so it is in place before
// the app makes its first call), a banner that says so, and the notifications a phone would get, shown in the page.
import { onNotice, startRun } from './mock.js'
import '../src/main.js'

const el = (html) => {
  const t = document.createElement('template')
  t.innerHTML = html.trim()
  return t.content.firstChild
}

document.head.append(
  el(`<style>
  .demo-bar { position: fixed; z-index: 50; left: 50%; bottom: 16px; transform: translateX(-50%); width: max-content; max-width: calc(100vw - 24px);
    display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; padding: 10px 12px 10px 16px; border: 1px solid var(--rule);
    border-radius: 999px; background: var(--panel); color: var(--ink); box-shadow: 0 6px 24px rgb(0 0 0 / 0.12); font-size: 0.875rem; }
  .demo-bar b { font-weight: 700; }
  .demo-bar .muted { color: var(--muted); }
  .demo-bar button, .demo-bar a { font: inherit; padding: 5px 12px; border-radius: 999px; border: 1px solid var(--rule); background: var(--panel); color: var(--ink); text-decoration: none; cursor: pointer; }
  .demo-bar .go { background: var(--ink); color: var(--panel); border-color: var(--ink); }
  .demo-bar .x { padding: 5px 9px; border: 0; color: var(--muted); }
  @media (max-width: 640px) { .demo-bar { border-radius: var(--radius); bottom: 10px; } .demo-bar .why { flex-basis: 100%; } }
  body { padding-bottom: 72px; }
  .demo-notes { position: fixed; z-index: 60; top: 12px; right: 12px; width: min(360px, calc(100vw - 24px)); display: grid; gap: 8px; }
  .demo-note { display: grid; grid-template-columns: auto 1fr; gap: 2px 10px; align-items: start; padding: 10px 14px; border: 1px solid var(--rule);
    border-radius: 12px; background: var(--panel); color: var(--ink); box-shadow: 0 8px 28px rgb(0 0 0 / 0.16); text-align: left; font: inherit;
    font-size: 0.875rem; cursor: pointer; animation: demo-in 0.25s ease-out; }
  .demo-note svg { grid-row: span 3; width: 22px; height: 22px; margin-top: 2px; }
  .demo-note small { color: var(--muted); }
  .demo-note.error { border-color: var(--failed); }
  .demo-note.warn { border-color: var(--crashed); }
  @keyframes demo-in { from { opacity: 0; transform: translateY(-8px); } }
  @media (prefers-reduced-motion: reduce) { .demo-note { animation: none; } }
</style>`),
)

const bar = el(`<div class="demo-bar" role="region" aria-label="About this demo">
  <span class="why"><b>Demo.</b> <span class="muted">Made-up runs, training in your browser.</span></span>
  <button class="go" type="button">Start a run</button>
  <a href="/">Get runyard</a>
  <button class="x" type="button" aria-label="Hide this bar">✕</button>
</div>`)
bar.querySelector('.go').onclick = () => (location.hash = '#/r/' + startRun())
bar.querySelector('.x').onclick = () => bar.remove()
document.body.append(bar)

// Notifications: what ntfy, Slack or web push would bring to your phone, here for twelve seconds each.
const notes = el('<div class="demo-notes" aria-live="polite"></div>')
document.body.append(notes)
const mark = '<svg viewBox="0 0 256 256" aria-hidden="true"><path fill="currentColor" d="M36 220 V144 A108 108 0 0 1 144 36 H220 L184 72 H144 A72 72 0 0 0 72 144 V220 Z"/><path fill="var(--running)" d="M96 220 V144 A48 48 0 0 1 144 96 H220 L184 132 H144 A12 12 0 0 0 132 144 V220 Z"/></svg>'
onNotice((n) => {
  const note = el(`<button type="button" class="demo-note ${n.level}">${mark}<small>runyard · now</small><b></b><span></span></button>`)
  note.querySelector('b').textContent = n.title
  note.querySelector('span').textContent = n.body
  note.onclick = () => {
    if (n.run) location.hash = '#/r/' + n.run
    note.remove()
  }
  notes.prepend(note)
  while (notes.children.length > 3) notes.lastChild.remove()
  setTimeout(() => note.remove(), 12000)
})
