# runyard design

The web UI (`web/`) and the iOS app share one look: a quiet, neutral dashboard where colour means state. Everything below is a token or a rule; `web/src/app.css` is the source of truth for the values, this file is the source of truth for the reasoning. Do not add a button, card, radius or colour that is not listed here; change the token instead.

Sources, in order of weight: the [taste-skill](https://github.com/Leonxlnx/taste-skill) app-screen and colour rules, the [Apple Human Interface Guidelines](https://developer.apple.com/design/human-interface-guidelines) for touch and platform conventions, and [Mebius charts](https://www.mebius.design/freebies/charts) for the chart idiom. Reviewed and not adopted: [Inter](https://rsms.me/inter) (the app keeps Atkinson Hyperlegible, which is already bundled and more legible for digits), Midjourney and Ditther (the UI has no imagery), Book of Shapes (no decorative patterns), beautifului.dev (chat-agent primitives, not a tracker).

## Tokens

Colours are declared once with `light-dark()`; `<html data-theme="light|dark">` overrides the OS setting.

| token | light | dark | role |
|---|---|---|---|
| `--paper` | `#f9fafb` | `#09090b` | the ground, 70–90% of every screen |
| `--panel` | `#ffffff` | `#121214` | one tone step up: cards, tables, inputs |
| `--soft` | `#f3f4f6` | `#1c1c1f` | hover, the active nav item, progress tracks |
| `--rule` | `#e5e7eb` | `#27272a` | the one hairline: panel edges, table rows, chart grid |
| `--ink` | `#18181b` | `#fafafa` | text and the primary button |
| `--muted` | `#6b7280` | `#a1a1aa` | secondary text; 4.5:1 on the ground |
| `--running` | `#0f766e` | `#2dd4bf` | state: alive; 4.5:1 as a word on a panel |
| `--finished` | `#52525b` | `#a1a1aa` | state: done, deliberately quiet |
| `--failed` | `#dc2626` | `#f87171` | state: error |
| `--crashed` | `#b45309` | `#fbbf24` | state: silent or stuck; 4.5:1 as a word on a panel |
| `--c1`…`--c6` | blue, teal, red, violet, amber, green | lighter tints | chart series only, in this order |
| `--radius` | `8px` | | panels, cards, charts |
| `--radius-sm` | `6px` | | buttons, inputs, tags |
| `--sans` | Atkinson Hyperlegible Next | | all text |
| `--mono` | Atkinson Hyperlegible Mono | | code, config keys, log output |

Rules: one accent is state colour, nothing else is coloured. No gradients, no glows, no shadows on anything at rest (a menu or dialog may float). No violet or indigo as an accent; `--c4` exists only so six series stay distinct. A raw hex in a component is a second palette starting.

## Type

- Sizes per screen: four to six. Page title 24px/700, card and run name 15–16px/600, body 15px/400, labels and table headers 13px/400, KPI value 28px/700, axis ticks 12px. Nothing under 11px.
- Tracking 0; negative (−0.02em) only on the page title and KPI values.
- Section headings are sentence case at 13px in `--muted`, never uppercase or letter-spaced.
- Tabular numerals everywhere (`font-variant-numeric: tabular-nums` on `body`). Numbers in tables are right-aligned with a fixed number of decimals.
- Monospace only for code, config keys and logs.

## Layout

- Sidebar 232px, rows 32px tall, 14px labels at weight 400, 18px outline icons at 1.8px stroke in `--muted`. The active item is a `--soft` pill, radius `--radius-sm`, no accent bar.
- Content gutter 32px on desktop, 16px on phones; content max width 1280px.
- One gap value per grid, both axes: 14px between cards, 24px between sections. Cards in one row share a bottom edge.
- Below 768px the sidebar folds into a drawer behind a 48px top bar with a menu button. Touch targets are at least 44px tall; the drawer never exceeds 85% of the width.

## Surfaces and controls

- A panel is `--panel` with a 1px `--rule` edge, radius `--radius`. Never a border and a shadow together, never a panel inside a panel.
- Buttons are 32px tall, 14px/500, radius `--radius-sm`. One filled primary per screen (`--ink` on `--paper`). Secondaries are `--panel` with a `--rule` border. Pressed or selected is the primary style.
- Tabs are text with a 2px `--ink` underline on the active one, wrapping rather than scrolling.
- Inputs are 40px tall, `--paper` fill, 1px `--rule` border, radius `--radius-sm`.
- Tags are 1px `--rule` outlines at 12px, radius `--radius-sm`. Status is a dot plus a word in the state colour; the running dot breathes, nothing else moves except on press, hover and focus.
- Focus ring: `outline: 2px solid var(--c1); outline-offset: 2px`.

## Tables and lists

- Header row: 13px/400 `--muted`, no fill band. Rows 44px, 1px `--rule` between rows, no zebra striping. Hover is `--soft`.
- Identity column (run name) takes a third to a half of the width; the host sits under the name as a 13px subline.
- Overview lists show 3–8 rows with a "View all" link and no pagination.
- A missing value is a grey dash, never an empty cell or a full-size "–".

## What a run looks like at a glance

- A running run's card reads top to bottom: project and state, name, the main metric's value at KPI size with
  its curve, the newest sample images at the right at the same height, a 4px progress bar in the state colour
  with "step N / M · P%" and the time left under it, then elapsed time, host and heartbeat in muted.
- Where there is a total, progress is a bar as well as a sentence; a finished run shows the sentence only.
- A list row says how a run ended only when it did not finish: the word in its state colour, then project and age.
- Samples of every key move on one scrubber, side by side, so each prompt is seen at the same step; the caption
  under an image is its key and its prompt, never the step again.
- Below 640px a table of runs is a list: name and state on one line, when it ran and the leading metric on the
  next, tags under them. Nothing scrolls sideways on a phone.

## Charts

- The loss leads: charts are ordered losses first, then by name.

- One hue per chart; multi-run comparisons use `--c1`…`--c6` in order with a dot legend, five entries at most.
- Line 1.5px, straight segments, no point markers. The min–max band of a bucketed series is the line colour at 14%.
- Gridlines: 1px `--rule`, three to five horizontal, no y-axis line, no x-axis line. Ticks 12px `--muted`, no tick marks. X labels never rotate.
- The hover readout is plain text in the chart caption (label and value on one line), not a floating box.
- Plot height 170px in grids, 36px for a sparkline. Sparklines have no axes and no fill.
- Empty chart: the zero line and the empty range, no illustration.

## States

- Empty section: keeps its heading, shows one sentence in a dashed 1px `--rule` box, radius `--radius`, about 38px tall.
- Error: inside the failed block as a sentence; the failed colour appears only on the word that names the failure.
- Loading: nothing flashes; blocks render when their data arrives, with no layout shift.
- Live data arrives over SSE; a 7px dot next to the brand is `--running` when connected and `--crashed` while reconnecting.
